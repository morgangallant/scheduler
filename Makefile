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

