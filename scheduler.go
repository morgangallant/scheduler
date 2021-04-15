package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/joho/godotenv"
	"github.com/morgangallant/scheduler/prisma/db"
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
	scheduler := newScheduler(client, secret(), endpoint())
	webServer := newWebServer(":"+port(), scheduler)
	return runServers(scheduler, webServer)
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
	close    chan struct{}
	secret   string
	endpoint string
}

func newScheduler(client *db.PrismaClient, secret, endpoint string) *scheduler {
	return &scheduler{
		client:   client,
		recomp:   make(chan struct{}),
		close:    make(chan struct{}),
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

func (s *scheduler) deleteJob(ctx context.Context, id string) error {
	if _, err := s.client.Job.FindUnique(
		db.Job.ID.Equals(id),
	).Delete().Exec(ctx); err != nil {
		return err
	}
	log.Printf("Deleted job %s.", id)
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
		if err := s.deleteJob(ctx, job.ID); err != nil {
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
	s.close <- struct{}{}
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
	return created.ID, nil
}

type webs struct {
	addr       string
	mux        *http.ServeMux
	sched      *scheduler
	underlying *http.Server
}

func newWebServer(addr string, s *scheduler) *webs {
	ws := &webs{addr: addr, mux: http.NewServeMux(), sched: s}
	ws.mux.HandleFunc("/", ws.rootHandler())
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
		if err := ws.sched.deleteJob(r.Context(), req.JobID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
}
