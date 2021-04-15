-- CreateTable
CREATE TABLE "Job" (
    "id" TEXT NOT NULL,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "scheduledFor" TIMESTAMP(3) NOT NULL,
    "endpoint" TEXT NOT NULL,
    "body" JSONB,

    PRIMARY KEY ("id")
);

-- CreateIndex
CREATE INDEX "Job.scheduledFor_index" ON "Job"("scheduledFor");
