FROM golang:1.16 as build

ADD . /scheduler
WORKDIR /scheduler

# Do the database migrations and code-generation.
ARG DATABASE_URL
RUN go get github.com/prisma/prisma-client-go
RUN go run github.com/prisma/prisma-client-go db push --preview-feature

# Build the scheduler.
RUN go build -o scheduler .

# Copy the scheduler binary to smaller container for deployment.
FROM gcr.io/distroless/base
WORKDIR /scheduler
COPY --from=build /scheduler/scheduler /scheduler/
ENTRYPOINT ["/scheduler/scheduler"]
