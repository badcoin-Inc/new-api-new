# PostgreSQL Backup

`cmd/postgres-backup` creates a PostgreSQL custom-format dump, uploads it to Cloudflare R2, and optionally sends the backup link to a Feishu bot.

## Run

For Docker Compose deployments, the image includes `/postgres-backup`. Run the one-shot service:

```bash
docker compose --profile tools run --rm postgres-backup
```

For the dev compose stack:

```bash
docker compose -f docker-compose.dev.yml --profile tools run --rm postgres-backup
```

Use cron or your scheduler to run it once per day during off-peak hours.

```cron
0 4 * * * cd /path/to/new-api && docker compose --profile tools run --rm postgres-backup >> logs/postgres-backup.log 2>&1
```

Copy the example environment file to the repository root before running the Docker service:

```bash
cp new-api-postgres-backup.env.example ./new-api-postgres-backup.env
```

## Environment

- `POSTGRES_DSN`: PostgreSQL connection URL. The compose service sets this automatically.
- `POSTGRES_BACKUP_OUTPUT_DIR`: Local output directory. Compose uses `/data/postgres-backups`.
- `POSTGRES_BACKUP_DATE`: Optional date used in the filename. Default: today, format `YYYY-MM-DD`.
- `POSTGRES_BACKUP_TIMEOUT_MINUTES`: Whole job timeout. Default: `60`.
- `POSTGRES_BACKUP_R2_BUCKET`: R2 bucket for PostgreSQL backups.
- `POSTGRES_BACKUP_R2_OBJECT_PREFIX`: R2 object prefix. Default: `daily/postgres`.
- `POSTGRES_BACKUP_KEEP_LOCAL`: Keep local dump after upload. Default: `false`.
- `R2_ACCOUNT_ID`, `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY`: R2 credentials.
- `R2_PUBLIC_BASE_URL`: Optional public/custom domain base URL.
- `FEISHU_WEBHOOK_URL`: Optional Feishu bot webhook URL.
- `FEISHU_KEYWORD`: Optional keyword prepended to the message if the bot enables keyword validation.
