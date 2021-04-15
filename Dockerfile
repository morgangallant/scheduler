FROM golang:1.16 as build

ADD . /scheduler
WORKDIR /scheduler

ARG DATABASE_URL

RUN go get github.com/prisma/prisma-client-go
RUN go run github.com/prisma/prisma-client-go generate
RUN go run github.com/prisma/prisma-client-go db push --preview-feature
RUN go build -o scheduler .

FROM gcr.io/distroless/base
WORKDIR /scheduler
COPY --from=build /scheduler/scheduler /scheduler/
ENTRYPOINT ["/scheduler/scheduler"]
