usage:
	@cat Makefile

# Create a migration from any changes to prisma/schema.prisma, apply it to DATABASE_URL, and re-generate code.
migrate:
	go run github.com/prisma/prisma-client-go migrate dev --preview-feature

# Generate the Go language bindings for the schema.
generate:
	go run github.com/prisma/prisma-client-go generate

# Reset DATABASE_URL (deleting all data), then apply all migrations.
reset:
	go run github.com/prisma/prisma-client-go migrate reset --preview-feature

# Run the application locally.
run:
	go run .

# Docker commands - same as above, but running inside a docker container.
docker-run:
	docker run -it --network="host" -v ${PWD}:/scheduler -w /scheduler golang:1.16.3 make run
docker-reset:
	docker run -it --network="host" -v ${PWD}:/scheduler -w /scheduler golang:1.16.3 make reset
docker-generate:
	docker run -it --network="host" -v ${PWD}:/scheduler -w /scheduler golang:1.16.3 make generate
docker-migrate:
	docker run -it --network="host" -v ${PWD}:/scheduler -w /scheduler golang:1.16.3 make migrate
