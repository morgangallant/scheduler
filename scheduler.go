package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/joho/godotenv"
	"github.com/morgangallant/scheduler/prisma/db"
	"github.com/robfig/cron/v3"
)

func init() {
	// For development environments.
	_ = godotenv.Load()
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func port() string {
	if p, ok := os.LookupEnv("PORT"); ok {
		return p
	}
	return "8080"
}

func endpoint() string {
	if e, ok := os.LookupEnv("ENDPOINT"); ok {
		return e
	}
	panic("missing ENDPOINT environment variable")
}

func secret() string {
	if s, ok := os.LookupEnv("SECRET"); ok {
		return s
	}
	panic("missing SECRET environment variable")
}

func run() error {
	client := db.NewClient()
	if err := client.Prisma.Connect(); err != nil {
		return err
	}
	defer client.Disconnect()
	secret, endpoint := secret(), endpoint()
	scheduler := newScheduler(client, secret, endpoint)
	cs := newCrons(client, secret, endpoint)
	webServer := newWebServer(":"+port(), scheduler, cs)
	return runServers(scheduler, cs, webServer)
}

type server interface {
	start() error
	stop()
}

func runServers(servers ...server) error {
	c := make(chan error, 1)
	for _, server := range servers {
		s := server
		go func() {
			c <- s.start()
		}()
	}
	err := <-c
	for _, server := range servers {
		server.stop()
	}
	return err
}

type scheduler struct {
	client   *db.PrismaClient
	recomp   chan struct{}
	secret   string
	endpoint string
}

func newScheduler(client *db.PrismaClient, secret, endpoint string) *scheduler {
	return &scheduler{
		client:   client,
		recomp:   make(chan struct{}),
		secret:   secret,
		endpoint: endpoint,
	}
}

const headerSecretKey = "Scheduler-Secret"

func (s *scheduler) executeJob(job db.JobModel) error {
	var rdr io.Reader
	body, ok := job.Body()
	if ok {
		rdr = bytes.NewBuffer(body)
	}
	req, err := http.NewRequest("POST", s.endpoint, rdr)
	if err != nil {
		return err
	}
	req.Header.Set(headerSecretKey, s.secret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("job failed with non-ok status code %d: %s", resp.StatusCode, resp.Status)
	}
	log.Printf("Executed job %s.", job.ID)
	return nil
}

func (s *scheduler) executePendingJobs() error {
	ctx := context.TODO()
	jobs, err := s.client.Job.FindMany(
		db.Job.ScheduledFor.BeforeEquals(db.DateTime(time.Now())),
	).Exec(ctx)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if err := s.executeJob(job); err != nil {
			log.Printf("Failed to execute job %s: %v", job.ID, err)
		}
		if _, err := s.client.Job.FindUnique(
			db.Job.ID.Equals(job.ID),
		).Delete().Exec(ctx); err != nil {
			return err
		}
	}
	log.Printf("Executed %d jobs.", len(jobs))
	return nil
}

func (s *scheduler) nextScheduledJob() (*time.Time, error) {
	jobs, err := s.client.Job.FindMany().
		OrderBy(db.Job.ScheduledFor.Order(db.ASC)).
		Take(1).
		Exec(context.TODO())
	if err != nil {
		return nil, err
	}
	if len(jobs) == 0 {
		return nil, nil
	}
	return &jobs[0].ScheduledFor, nil
}

func (s *scheduler) start() error {
	log.Println("Started scheduler.")
	for {
		log.Println("Scheduler woke up.")
		if err := s.executePendingJobs(); err != nil {
			return err
		}
		ts, err := s.nextScheduledJob()
		if err != nil {
			return err
		} else if ts == nil {
			log.Println("Scheduler waiting for job.")
			<-s.recomp
			continue
		}
		log.Printf("Scheduler sleeping until %s.", ts.String())
		select {
		case <-time.After(time.Until(*ts)):
		case <-s.recomp:
		}
	}
}

func (s *scheduler) stop() {
	log.Println("Closed scheduler.")
}

func (s *scheduler) createNewJob(ctx context.Context, on time.Time, body []byte) (string, error) {
	created, err := s.client.Job.CreateOne(
		db.Job.ScheduledFor.Set(db.DateTime(on)),
		db.Job.Body.Set(body),
	).Exec(ctx)
	if err != nil {
		return "", err
	}
	log.Printf("New job with id %s.", created.ID)
	s.recomp <- struct{}{}
	return created.ID, nil
}

func (s *scheduler) deleteFutureJob(ctx context.Context, id string) error {
	_, err := s.client.Job.FindUnique(
		db.Job.ID.Equals(id),
	).Delete().Exec(ctx)
	if errors.Is(err, db.ErrNotFound) {
		return nil
	} else if err != nil {
		return err
	}
	s.recomp <- struct{}{}
	log.Printf("Deleted job %s.", id)
	return nil
}

type crons struct {
	client   *db.PrismaClient
	recomp   chan struct{}
	endpoint string
	secret   string
}

func newCrons(client *db.PrismaClient, secret, endpoint string) *crons {
	return &crons{
		client:   client,
		recomp:   make(chan struct{}),
		endpoint: endpoint,
		secret:   secret,
	}
}

type cronJob struct {
	JobID string `json:"id"`
	Spec  string `json:"spec"`
}

func (cs *crons) getCronJobs() ([]cronJob, error) {
	jobs, err := cs.client.Cron.FindMany().Exec(context.TODO())
	if err != nil {
		return nil, err
	}
	ret := make([]cronJob, 0, len(jobs))
	for _, job := range jobs {
		ret = append(ret, cronJob{
			JobID: job.ID,
			Spec:  job.Specification,
		})
	}
	return ret, nil
}

type cronJobRequest struct {
	JobID string `json:"cron_id"`
}

func (cs *crons) executeCronJob(id string) error {
	buf, err := json.Marshal(cronJobRequest{
		JobID: id,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", cs.endpoint, bytes.NewBuffer(buf))
	if err != nil {
		return err
	}
	req.Header.Set(headerSecretKey, cs.secret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("invalid status code %d returned: %s", resp.StatusCode, resp.Status)
	}
	return nil
}

func (cs *crons) clearCronJobs() error {
	if _, err := cs.client.Cron.FindMany().Delete().Exec(context.TODO()); err != nil {
		return err
	}
	return nil
}

func (cs *crons) insertCronJob(cj cronJob) error {
	if _, err := cs.client.Cron.CreateOne(
		db.Cron.Specification.Set(cj.Spec),
		db.Cron.ID.Set(cj.JobID),
	).Exec(context.TODO()); err != nil {
		return err
	}
	return nil
}

func (cs *crons) start() error {
	log.Println("Started crons.")
	for {
		client := cron.New(cron.WithSeconds())
		jobs, err := cs.getCronJobs()
		if err != nil {
			return err
		}
		for _, job := range jobs {
			j := job
			if _, err := client.AddFunc(j.Spec, func() {
				if err := cs.executeCronJob(j.JobID); err != nil {
					log.Printf("Failed to execute cron job %s (%s): %v", j.JobID, j.Spec, err)
					return
				}
				log.Printf("Executed cron job %s (%s).", j.JobID, j.Spec)
			}); err != nil {
				return err
			}
		}
		client.Start()
		log.Printf("Started crons w/ %d jobs.", len(jobs))
		<-cs.recomp
		log.Println("Got crons recompute request, tearing down.")
		client.Stop()
	}
}

func (cs *crons) stop() {
	log.Println("Closed crons.")
}

type webs struct {
	addr       string
	mux        *http.ServeMux
	sched      *scheduler
	cs         *crons
	underlying *http.Server
}

func newWebServer(addr string, s *scheduler, cs *crons) *webs {
	ws := &webs{addr: addr, mux: http.NewServeMux(), sched: s, cs: cs}
	ws.mux.HandleFunc("/", ws.rootHandler())
	ws.mux.HandleFunc("/cron", ws.cronHandler())
	ws.mux.HandleFunc("/insert", ws.insertHandler())
	ws.mux.HandleFunc("/delete", ws.deleteHandler())
	return ws
}

func (ws *webs) start() error {
	if ws.underlying != nil {
		ws.stop()
	}
	ws.underlying = &http.Server{
		Addr:         ws.addr,
		Handler:      ws.mux,
		ReadTimeout:  time.Second,
		WriteTimeout: time.Second * 10,
	}
	log.Println("Started web server.")
	return ws.underlying.ListenAndServe()
}

func (ws *webs) stop() {
	if ws.underlying == nil {
		return
	}
	ws.underlying.Shutdown(context.Background())
	ws.underlying = nil
	log.Println("Closed web server.")
}

func (ws *webs) rootHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "Scheduler (github.com/morgangallant/scheduler) written by Morgan Gallant.")
	}
}

func (ws *webs) insertHandler() http.HandlerFunc {
	type request struct {
		Timestamp time.Time       `json:"timestamp"`
		Body      json.RawMessage `json:"body"`
	}
	type response struct {
		JobID string `json:"id"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if secret := r.Header.Get(headerSecretKey); secret != ws.sched.secret {
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
		var req request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		jid, err := ws.sched.createNewJob(r.Context(), req.Timestamp, req.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := json.NewEncoder(w).Encode(response{JobID: jid}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

func (ws *webs) deleteHandler() http.HandlerFunc {
	type request struct {
		JobID string `json:"id"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if secret := r.Header.Get(headerSecretKey); secret != ws.sched.secret {
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
		var req request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := ws.sched.deleteFutureJob(r.Context(), req.JobID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
}

func (ws *webs) cronHandler() http.HandlerFunc {
	type request struct {
		Jobs []cronJob `json:"jobs"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if secret := r.Header.Get(headerSecretKey); secret != ws.sched.secret {
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
		var req request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := ws.cs.clearCronJobs(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		for _, job := range req.Jobs {
			if err := ws.cs.insertCronJob(job); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		ws.cs.recomp <- struct{}{} // Signal to the crons that it needs to recompute.
	}
}
