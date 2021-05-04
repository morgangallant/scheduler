-- CreateTable
CREATE TABLE "Cron" (
    "id" TEXT NOT NULL,
    "createdAt" TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "specification" TEXT NOT NULL,

    PRIMARY KEY ("id")
);
