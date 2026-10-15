# Data E2E testing: akun, scope, dan SLA/maturity

Dokumen ini menjelaskan data yang dibuat oleh `make db-reset` (`ddl/seed_v3.sql` + `ddl/seed_scoring.sql`) dan skenario uji per role.

## 1. Menyiapkan data

Cukup satu file: **[ddl/e2e_reset.sql](../ddl/e2e_reset.sql)**. File ini membuat tabel SLA bila belum ada, menghapus dan mengisi ulang semua data, mengisi aturan SLA/maturity, dan menjadikan `pipelines.site_id` wajib.

- **DBeaver:** buka `ddl/e2e_reset.sql`, jalankan sebagai script (Alt+X), lalu jalankan server (`make run`). Job scoring di server langsung menghitung fakta dan membekukan periode yang sudah selesai.
- **Terminal:** `make e2e-reset` (menjalankan file tsb sekaligus `make scoring-backfill`), lalu `make run`.

File ini dihasilkan dari migration 007, `seed_v3.sql`, dan `seed_scoring.sql`; setelah mengubah salah satunya, buat ulang dengan `make e2e-sql`.

- Semua akun memakai password `Password123!`.
- Data laporan dimulai dari **awal kuartal lalu** sampai hari ini. Contoh jika dijalankan 15 Oktober 2026: data 1 Juli – 15 Oktober 2026.
- Pembagian skenario laporan (tepat waktu, telat, upload susulan, dan seterusnya) **deterministik**: reset pada tanggal yang sama selalu menghasilkan data dan skor yang sama.
- Tanggal di bawah memakai contoh tanggal reset **15 Oktober 2026**. Jika reset dilakukan di tanggal lain, semua tanggal ikut bergeser.

## 2. Akun dan scope

### Super Admin

| Username | Scope | Bisa melihat |
|---|---|---|
| `superadmin` | Tanpa scope (semua) | Semua vendor, site, worker |

### Admin (scope: site)

| Username | Site | Vendor di site tsb |
|---|---|---|
| `admin.paiton` | PLTU Paiton | VND-001 PT Mitra Teknik Abadi |
| `admin.pacitan` | PLTU Pacitan | VND-001 |
| `admin.suralaya` | PLTU Suralaya | VND-002 PT Karya Mandiri Sejahtera |
| `admin.labuan` | PLTU Labuan | VND-002 |
| `admin.tjb` | PLTU Tanjung Jati B | VND-003 CV Sinergi Perkasa |
| `admin.rembang` | PLTU Rembang | VND-003 |
| `admin.cirebon` | PLTU Cirebon | VND-004 PT Bumi Jaya Konstruksi |
| `admin.indramayu` | PLTU Indramayu | VND-004 |
| `admin.plbratu` | PLTU Pelabuhan Ratu | VND-005 PT Cahaya Energi Nusantara |
| `admin.adipala` | PLTU Adipala | VND-005 |
| `admin.regional` | PLTU Paiton **dan** PLTU Suralaya | VND-001 dan VND-002 |

### Admin Worker (scope: vendor)

| Username | Vendor |
|---|---|
| `aw.mitra` | VND-001 |
| `aw.karya` | VND-002 |
| `aw.sinergi` | VND-003 |
| `aw.bumi` | VND-004 |
| `aw.cahaya` | VND-005 |

### Worker (scope: vendor)

Username `w.vnd<vendor>.<nomor>`, contoh `w.vnd001.01` sampai `w.vnd005.10`. Setiap vendor punya 10 worker:

| Worker | Site | Shift laporan | Tugas |
|---|---|---|---|
| 01–05 | Site pertama vendor (urut kode) | 01, 04: DAY; 02, 05: EVENING; 03: NIGHT | Semua lokasi dari area ke-1 s/d ke-5 (area diurutkan per nama) |
| 06–10 | Site kedua vendor | 07, 10: DAY; 06, 09: NIGHT; 08: EVENING | Semua lokasi dari area ke-1 s/d ke-5 |

Site pertama dan kedua per vendor:

| Vendor | Site pertama (worker 01–05) | Site kedua (worker 06–10) |
|---|---|---|
| VND-001 | PACITAN | PAITON |
| VND-002 | LABUAN | SURALAYA |
| VND-003 | REMBANG | TJB |
| VND-004 | CIREBON | INDRAMAYU |
| VND-005 | ADIPALA | PLBRATU |

## 3. Aturan SLA dan periode hasil

| Vendor | Aturan default (cakupan VENDOR + site tanpa override) | Override site |
|---|---|---|
| VND-001 | **MONTH**, standar | PAITON: **WEEK**, ketat |
| VND-002 | **WEEK**, standar | SURALAYA: **QUARTER**, ketat |
| VND-003 | **QUARTER**, ketepatan waktu (`AVG_LATE_MINUTES`, `LATE_UPLOAD_RATE`) | REMBANG: **MONTH**, ketat |
| VND-004 | **MONTH**, standar | – |
| VND-005 | **WEEK**, dengan temuan K3 (`K3_FINDING_NOTES_RATE`) | ADIPALA: **QUARTER**, ketat |

Set indikator:

| Set | Indikator (bobot / target) |
|---|---|
| standar | COMPLETION 40 / 95, ON_TIME 40 / 90, EVIDENCE 20 / 98 |
| ketat | COMPLETION 40 / 97, ON_TIME 40 / 95, EVIDENCE 20 / 99 |
| ketepatan waktu | COMPLETION 30 / 95, ON_TIME 30 / 90, AVG_LATE_MINUTES 20 / 15 menit, LATE_UPLOAD 20 / 10% |
| dengan temuan K3 | COMPLETION 35 / 95, ON_TIME 35 / 90, EVIDENCE 15 / 98, K3_FINDING_NOTES 15 / 100 |

Hasil periode per cakupan (perhatikan setiap jenis periode muncul di ketiga cakupan):

| Periode | Cakupan VENDOR | Cakupan SITE dan WORKER di site |
|---|---|---|
| **WEEK** | VND-002, VND-005 | PAITON, LABUAN, PLBRATU |
| **MONTH** | VND-001, VND-004 | PACITAN, REMBANG, CIREBON, INDRAMAYU |
| **QUARTER** | VND-003 | SURALAYA, TJB, ADIPALA |

Maturity: satu aturan default per vendor dengan periode yang sama seperti SLA default vendor tsb (VND-001/004 bulanan, VND-002/005 mingguan, VND-003 kuartalan) dan level default.

Periode yang sudah dibekukan setelah `make scoring-backfill` (contoh reset 15 Oktober 2026):

| Periode | Dibekukan | Masih live |
|---|---|---|
| WEEK | 29 Jun – 5 Jul s/d 5 – 11 Okt | 12 – 18 Okt |
| MONTH | Juli, Agustus, September | Oktober |
| QUARTER | Q3 (Jul–Sep) | Q4 |

Minggu pertama (29 Jun – 5 Jul) hanya berisi data 1–5 Juli karena penugasan dimulai 1 Juli.

Tren per vendor (dibuat sengaja agar skor berbeda): VND-001 stabil baik, VND-002 membaik, VND-003 menurun, VND-004 sedang, VND-005 paling lemah.

## 4. Skenario uji per role

Login dulu di `POST /api/v1/auth/login`, lalu pakai `access_token`.

ID yang dipakai di skenario (`<id ...>`) bisa diambil dengan:

```sql
SELECT 'vendor' AS jenis, code AS kode, name AS nama, id::text FROM vendors
UNION ALL
SELECT 'site', code, name, id::text FROM sites
UNION ALL
SELECT 'user', u.username, coalesce(u.full_name, u.username), u.id::text
FROM users u
WHERE u.username IN ('superadmin', 'admin.paiton', 'admin.regional', 'aw.mitra', 'w.vnd001.01', 'w.vnd001.06')
ORDER BY 1, 2;
```

### Super Admin (`superadmin`)

| # | Langkah | Hasil yang diharapkan |
|---|---|---|
| SA-1 | `GET /sla-scores?scope=VENDOR&from=2026-06-01` | Hasil untuk 5 vendor: VND-002/005 per minggu, VND-001/004 per bulan, VND-003 per kuartal |
| SA-2 | `GET /sla-scores?scope=SITE&from=2026-06-01` | Hasil untuk 10 site dengan jenis periode sesuai tabel di bagian 3 |
| SA-3 | `GET /sla-scores?scope=WORKER&worker_id=<id w.vnd001.06>` | Hasil mingguan di PAITON (aturan override) |
| SA-4 | `GET /sla-scores/live?scope=SITE&site_id=<id SURALAYA>` | Satu hasil kuartal berjalan untuk VND-002 dengan aturan ketat |
| SA-5 | `POST /sla-rules` default untuk VND-004 (`site_id: null`) | 201, versi 2; versi 1 punya `superseded_at` |
| SA-6 | `GET /maturity-levels?scope=VENDOR` | Hasil untuk 5 vendor, masing-masing dengan `level` 1–5 dan skor per dimensi; periode mengikuti aturan maturity vendor |

### Admin (`admin.paiton`)

| # | Langkah | Hasil yang diharapkan |
|---|---|---|
| AD-1 | `GET /sla-scores?scope=VENDOR` | Hanya VND-001 |
| AD-2 | `GET /sla-scores?scope=SITE` | Hanya site PAITON (mingguan) |
| AD-3 | `GET /sla-scores?scope=WORKER` | Hanya worker 06–10 VND-001 di PAITON |
| AD-4 | `GET /sla-rules` | Aturan VND-001 (default MONTH + override PAITON WEEK) |
| AD-5 | `POST /sla-rules` dengan `site_id` PAITON | 201, versi baru override PAITON |
| AD-6 | `POST /sla-rules` dengan `site_id: null` | 403 (default vendor hanya Super Admin) |
| AD-7 | `POST /sla-rules` dengan `site_id` PACITAN | 403 (di luar scope) |
| AD-8 | `GET /sla-scores?scope=SITE&site_id=<id SURALAYA>` | Daftar kosong |

### Admin multi-site (`admin.regional`)

| # | Langkah | Hasil yang diharapkan |
|---|---|---|
| AR-1 | `GET /sla-scores?scope=VENDOR` | VND-001 (bulanan) dan VND-002 (mingguan) |
| AR-2 | `GET /sla-scores?scope=SITE` | PAITON (mingguan) dan SURALAYA (kuartalan) |
| AR-3 | `POST /sla-rules` override SURALAYA untuk VND-002 | 201 |
| AR-4 | `POST /sla-rules` override LABUAN untuk VND-002 | 403 |

### Admin Worker (`aw.mitra`)

| # | Langkah | Hasil yang diharapkan |
|---|---|---|
| AW-1 | `GET /sla-scores?scope=VENDOR` | Hanya VND-001 |
| AW-2 | `GET /sla-scores?scope=SITE` | PACITAN (bulanan) dan PAITON (mingguan) |
| AW-3 | `GET /sla-scores/live?scope=WORKER` | 10 worker VND-001, masing-masing dengan periode site-nya |
| AW-4 | `GET /sla-rules` | 403 (hanya Super Admin dan Admin yang mengelola aturan) |
| AW-5 | `GET /sla-scores?scope=VENDOR&vendor_id=<id VND-002>` | Daftar kosong |

### Worker (`w.vnd001.01` dan `w.vnd001.06`)

| # | Langkah | Hasil yang diharapkan |
|---|---|---|
| WK-1 | `w.vnd001.01`: `GET /sla-scores?scope=WORKER` | Hanya dirinya, bulanan di PACITAN |
| WK-2 | `w.vnd001.06`: `GET /sla-scores?scope=WORKER` | Hanya dirinya, mingguan di PAITON |
| WK-3 | `GET /sla-scores/live?scope=WORKER&worker_id=<id worker lain>` | Tetap hanya hasil dirinya sendiri |
| WK-4 | `GET /sla-scores?scope=VENDOR` | 403 |
| WK-5 | `GET /maturity-levels?scope=WORKER` | Hanya dirinya |

### Validasi umum

| # | Langkah | Hasil yang diharapkan |
|---|---|---|
| V-1 | `GET /sla-scores/live?scope=VENDOR&worker_id=<id>` | 400 |
| V-2 | `POST /sla-rules` dengan total bobot 90 | 400 |
| V-3 | `POST /sla-rules` dengan indikator `CONSISTENCY` | 400 (khusus maturity) |
| V-4 | `POST /maturity-rules` tanpa `levels` | 201, level default terisi |

## 5. Memeriksa angka

Skor dihitung server, jadi nilainya tidak dicantumkan di sini. Untuk mencocokkan respons API dengan data yang tersimpan:

```sql
SELECT r.kind, r.scope_type, v.code AS vendor, s.code AS site, u.username AS worker,
       r.period_type, r.period_start, r.status, r.score, r.compliant, r.level, r.task_count
FROM score_period_results r
JOIN vendors v ON v.id = r.vendor_id
LEFT JOIN sites s ON s.id = r.site_id
LEFT JOIN users u ON u.id = r.worker_id
ORDER BY r.kind, r.scope_type, v.code, s.code NULLS FIRST, u.username NULLS FIRST, r.period_start;
```

Jumlah hasil per cakupan dan periode (untuk `kind = SLA`, setiap `scope_type` harus punya baris `WEEK`, `MONTH`, dan `QUARTER`):

```sql
SELECT kind, scope_type, period_type, count(*)
FROM score_period_results
GROUP BY 1, 2, 3
ORDER BY 1, 2, 3;
```
