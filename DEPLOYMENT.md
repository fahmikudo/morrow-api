# Deployment — morrow-api

GitHub Actions menjalankan vet, build, dan test, membangun image, mendorongnya
ke GHCR, lalu server cukup **menarik image dan menjalankan `docker compose up -d`**.
Server tidak pernah mengompilasi apa pun dan tidak pernah melihat source code.

Repo ini adalah backend. Frontend punya pipeline dan stack terpisah di repo
`morrow-web`; keduanya berjalan di server yang sama dan dilayani nginx host.

## File

| Path | Kegunaan |
| --- | --- |
| `.github/workflows/ci.yml` | Vet/build/test → build & push GHCR → deploy lewat SSH (hanya tag `v*`) |
| `deploy/docker-compose.yml` | Stack yang dijalankan server (service `api`), disalin ke `/opt/morrow-api/` setiap rilis |
| `deploy/deploy.sh` | Disalin ke `/opt/morrow-api/deploy.sh` setiap rilis; pull, tulis tag, `up -d --wait` |
| `deploy/.env.example` | Contoh `/opt/morrow-api/.env` produksi |
| `deploy/nginx/morrow-api.conf` | File site nginx host |
| `Dockerfile` | Image API; migrasi berjalan otomatis sebelum server start |

## Image dan tag

```
ghcr.io/fahmikudo/morrow-api
```

Setiap build di `main` dan setiap rilis mendorong tag:

- `sha-<pendek>`: selalu. Ini yang dipakai untuk rollback.
- `edge`: ujung `main`.
- `X.Y.Z`, `X.Y`, `latest`: hanya dari tag rilis `vX.Y.Z`.

Merge ke `main` hanya mempublikasikan image. **Hanya tag `v*` yang men-deploy.**
Versi yang berjalan tertulis di `/opt/morrow-api/.env` pada baris
`MORROW_API_IMAGE_TAG`.

## Setup server, sekali saja

`/opt` milik root, jadi buat direktori sebagai pemilik user SSH yang dipakai
CI (`DEPLOY_USER`). Contoh dengan `deploy`:

```bash
sudo install -d -o deploy -g deploy -m 750 /opt/morrow-api
```

Kalau `DEPLOY_USER` bukan `deploy` (misal `ubuntu`), samakan pemiliknya dan
jangan pakai mode 777:

```bash
sudo chown -R ubuntu:ubuntu /opt/morrow-api
sudo chmod 750 /opt/morrow-api
```

User itu juga harus bisa menjalankan Docker, karena `deploy.sh` memanggil
`docker compose`:

```bash
sudo usermod -aG docker ubuntu
# lalu login ulang agar grup baru berlaku
id ubuntu | grep docker
```

`scp` dan `deploy.sh` menulis ke direktori tersebut, jadi pemilik direktori
harus user SSH itu — bukan sekadar `chmod 777` yang membuka akses ke semua user.

Salin file pertama kali dari laptop (setelah itu CI yang memperbarui compose dan
`deploy.sh`):

```bash
scp deploy/docker-compose.yml deploy/deploy.sh deploy@<server>:/opt/morrow-api/
scp deploy/.env.example deploy@<server>:/opt/morrow-api/.env
ssh deploy@<server> 'chmod +x /opt/morrow-api/deploy.sh && chmod 600 /opt/morrow-api/.env'
```

Isi `/opt/morrow-api/.env`:

- `MORROW_API_IMAGE_TAG` dibiarkan kosong: `deploy.sh` yang mengisinya.
- `APP_PORT` (bawaan `8990`) harus sama dengan `upstream` di site nginx dan belum
  dipakai di server (`ss -ltn | grep :8990`). Port di dalam container tetap `8080`.
- `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB` dan `DATABASE_URL` mengisi
  database yang berjalan sebagai service `db` di stack ini (lihat bagian
  Database di bawah).
- `CORS_ORIGIN` berisi origin frontend, misal `https://mrrw.my.id`.
- `S3_*` diisi dari dashboard Cloudflare R2: endpoint
  `https://<ACCOUNT_ID>.r2.cloudflarestorage.com`, bucket privat, access key ID
  dan secret dari token yang di-scope ke bucket itu. `S3_REGION=auto`,
  `S3_FORCE_PATH_STYLE=false`, `S3_AUTO_CREATE_BUCKET=false`.
- `APP_ENCRYPTION_KEY` base64 yang decode ke 32 byte; `make vapid` untuk pasangan
  kunci push.

Bucket R2 dibuat di dashboard, bukan oleh API. Izinkan CORS untuk `GET`, `HEAD`,
`PUT` dengan header `content-type` dari origin frontend.

## Database

PostgreSQL 16 berjalan sebagai service `db` di stack yang sama, dengan volume
`pgdata`. Port-nya tidak dipublikasikan; hanya container `api` yang menjangkaunya
lewat network compose. `api` menunggu `db` sehat sebelum start, lalu image
menjalankan `./bin/migrate up` sebelum server mendengarkan.

- Kredensial ada di `.env`: `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB`.
- `DATABASE_URL` memakai host `db`, mis.
  `postgres://secondbrain:<password>@db:5432/secondbrain?sslmode=disable`.
- Buat password sekali dengan `openssl rand -hex 24` sebelum stack pertama kali
  dijalankan. Mengubah `POSTGRES_PASSWORD` setelah volume terbentuk **tidak**
  mengubah password role; ubah lewat `ALTER ROLE` di dalam container.
- Migrasi tidak dijalankan terpisah; container `api` yang menerapkannya.

Backup dan restore:

```bash
cd /opt/morrow-api

# backup (custom format, aman untuk pg_restore)
docker compose exec -T db pg_dump -U secondbrain -d secondbrain -Fc > backup.dump

# restore ke database kosong
docker compose exec -T db pg_restore -U secondbrain -d secondbrain \
  --clean --if-exists < backup.dump
```

Managed Postgres (Neon/Supabase/RDS): hapus service `db` dan `depends_on` di
`deploy/docker-compose.yml`, lalu isi `DATABASE_URL` dengan URL penyedia
(`sslmode=require`). `POSTGRES_*` tidak dipakai lagi.

## Login GHCR

```bash
sudo -u deploy bash -c 'echo "$GHCR_READ_TOKEN" | docker login ghcr.io -u fahmikudo --password-stdin'
```

Tanpa akses `read:packages`, `docker compose pull` gagal dengan 401.

## Nginx

Cloudflare menangani TLS (mode Flexible); origin hanya HTTP port 80.

```bash
sudo cp deploy/nginx/morrow-api.conf /etc/nginx/sites-available/<api-site>.conf
sudo ln -s /etc/nginx/sites-available/<api-site>.conf /etc/nginx/sites-enabled/
sudo nginx -t && sudo systemctl reload nginx
```

Domain di file itu sudah `api.mrrw.my.id`.

## Secret dan variable GitHub

Environment **production** di repo `morrow-api`:

| Secret | Isi |
| --- | --- |
| `DEPLOY_HOST` | alamat server |
| `DEPLOY_PORT` | port SSH |
| `DEPLOY_USER` | `deploy` |
| `DEPLOY_SSH_KEY` | private key deploy |
| `DEPLOY_KNOWN_HOSTS` | hasil `ssh-keyscan -p <port> <host>` |

Repository variable (Settings → Variables) — opsional, hanya untuk smoke test:

| Variable | Isi |
| --- | --- |
| `API_URL` | origin publik API, misal `https://api.mrrw.my.id` |

## Rilis

```bash
git tag v1.0.0
git push origin v1.0.0
```

Workflow menjalankan test, membangun dan mendorong image, lalu lewat SSH:

1. menyalin `docker-compose.yml` dan `deploy.sh` ke `/opt/morrow-api/`;
2. menjalankan `/opt/morrow-api/deploy.sh MORROW_API_IMAGE_TAG 1.0.0 api`:
   pull → tulis tag ke `.env` → `docker compose up -d --wait` → prune;
3. smoke test `GET ${API_URL}/healthz` bila variable diisi.

Tag yang tidak ada di GHCR menghentikan deploy sebelum `.env` diubah, dan versi
lama tetap berjalan.

## Rollback

```bash
ssh deploy@<server>
cd /opt/morrow-api
sed -i 's|^MORROW_API_IMAGE_TAG=.*|MORROW_API_IMAGE_TAG=sha-xxxxxxx|' .env
docker compose up -d
```

## Mengecek deployment

```bash
ssh deploy@<server> 'cd /opt/morrow-api && docker compose ps'
curl -s http://127.0.0.1:8990/healthz   # di server
```

- `api` berstatus `healthy`.
- Log: `docker compose logs -f api`.
