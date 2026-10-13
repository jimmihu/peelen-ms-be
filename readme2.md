PRD — SafeOps 360: Authentication & Authorization
13 Oktober 2026
1. Ringkasan
Modul ini memastikan setiap pengguna SafeOps 360 masuk dengan aman dan hanya melihat data site atau vendor miliknya. Modul ini adalah fondasi Sprint 1; semua modul lain (master data, cleaning, safety, scoring) bergantung padanya.
Masalah yang diselesaikan
•           Satu site bekerja dengan beberapa vendor yang datanya tidak boleh saling terlihat.
•           Manajemen site perlu melihat semua vendor di site-nya, tetapi tidak site lain.
•           Setiap aksi sensitif harus bisa ditelusuri siapa pelakunya.
2. Ruang Lingkup
In scope
•           Login username + password dengan JWT (access token + refresh token)
•           Refresh token, logout, dan pencabutan sesi
•           Ganti password dan reset password oleh Super Admin
•           Kelola user, role, dan scope (site/vendor)
•           Middleware autentikasi dan otorisasi di backend Go/Gin
•           Audit log untuk login dan aksi sensitif
Out of scope (PRD terpisah)
•           Master data site, vendor, kontrak, area, worker
•           SSO / login Google / OTP
•           Self-registration user
•           Lupa password mandiri / self-service (dibahas di open questions)
3. Role & Scope
Akses ditentukan oleh dua hal: role (apa yang boleh dilakukan) dan scope (data mana yang boleh disentuh). Keduanya divalidasi di backend pada setiap request.
Role (code)
Pengguna
Scope data
Sumber scope
Super Admin (SUPER_ADMIN)
Engineering, developer, sysadmin
Semua site dan vendor
Tidak perlu baris scope
Admin (ADMIN)
Manajemen, PIC, perwakilan site
Site yang ditugaskan, termasuk semua vendor di site itu
user_scopes.site_id
Admin Worker (ADMIN_WORKER)
Perwakilan vendor
Data vendornya sendiri
user_scopes.vendor_id
Worker (WORKER)
Tenaga kerja
Tugas dan data pribadinya
user_scopes.vendor_id + user_id sendiri

Hak kelola akun
Aksi
Super Admin
Admin
Admin Worker
Worker
Buat/nonaktifkan Super Admin & Admin
Ya
Tidak
Tidak
Tidak
Buat/nonaktifkan Admin Worker
Ya
Tidak
Tidak
Tidak
Buat/nonaktifkan Worker
Ya
Tidak
Ya, vendor sendiri
Tidak
Atur scope user
Ya
Tidak
Tidak
Tidak
Reset password user lain
Ya
Tidak
Ya, worker vendor sendiri
Tidak
Ganti password sendiri
Ya
Ya
Ya
Ya
Lihat audit log
Ya
Tidak
Tidak
Tidak

4. User Stories
ID
Sebagai
Saya ingin
Supaya
US-01
Semua user
Login dengan username dan password
Bisa mengakses aplikasi sesuai role
US-02
Semua user
Tetap login tanpa memasukkan password berulang selama sesi aktif
Pekerjaan di lapangan tidak terganggu
US-03
Semua user
Logout dan mengakhiri sesi
Akun aman jika perangkat dipakai bersama
US-04
Semua user
Mengganti password sendiri
Akun tetap aman
US-05
Super Admin
Membuat user dan memberi role
Pengguna baru bisa mulai bekerja
US-06
Super Admin
Mengatur scope site/vendor setiap user
Akses data sesuai tanggung jawabnya
US-07
Super Admin
Menonaktifkan user dan mencabut semua sesinya
Mantan pengguna langsung kehilangan akses
US-08
Super Admin
Melihat audit log login dan perubahan akses
Aktivitas sensitif bisa ditelusuri
US-09
Admin Worker
Membuat akun worker untuk vendor saya
Worker baru bisa menerima tugas
US-10
Admin Worker
Reset password worker vendor saya
Worker yang lupa password bisa kembali bekerja
US-11
Admin
Hanya melihat data site yang menjadi tanggung jawab saya
Data site lain tetap terlindungi

5. Kebutuhan Fungsional
Angka durasi dan batas di bawah adalah usulan awal; finalnya ada di Open Questions.
5.1 Login
•           FR-01 User login dengan username + password. Username unik, 3–30 karakter (huruf kecil, angka, titik, underscore), tidak case-sensitive, dan tidak bisa diubah setelah dibuat.
•           FR-02 Login berhasil mengembalikan access token (JWT, usulan 15 menit) dan refresh token (usulan 7 hari).
•           FR-03 Access token memuat sub (user_id), roles, exp, dan jti; scope tidak disimpan di token, selalu dibaca dari database.
•           FR-04 User nonaktif (is_active = false) tidak bisa login.
•           FR-05 Pesan gagal login generik (“Username atau password salah”), tidak membedakan username tidak ada vs password salah.
•           FR-06 Setelah 5 kali gagal berturut-turut, akun dikunci 15 menit (dicatat di users.failed_login_count dan users.locked_until; counter di-reset saat login berhasil).
•           FR-07 Login berhasil memperbarui last_login_at; login berhasil dan gagal (user_id kosong bila username tidak ditemukan) dicatat di audit_logs.
5.2 Token & sesi
•           FR-08 Refresh token disimpan sebagai hash di refresh_tokens, tidak pernah dalam bentuk asli.
•           FR-09 Refresh menghasilkan access token baru dan merotasi refresh token (yang lama dicabut).
•           FR-10 Refresh token yang sudah dicabut tetapi dipakai lagi dianggap pencurian: semua sesi user itu dicabut.
•           FR-11 Logout mencabut refresh token sesi saat ini; “logout semua perangkat” mencabut semuanya.
•           FR-12 Menonaktifkan user, mengganti role, atau mengganti password mencabut semua refresh token user tersebut.
5.3 Password
•           FR-13 Password minimal 8 karakter, mengandung huruf dan angka.
•           FR-14 Password di-hash dengan bcrypt (cost ≥ 12) atau argon2id.
•           FR-15 User baru dan hasil reset password wajib ganti password saat login pertama (users.must_change_password = true).
5.4 Kelola user, role, scope
•           FR-16 Super Admin dapat membuat, mengubah, menonaktifkan user, dan mengaktifkan kembali.
•           FR-17 Satu user boleh punya lebih dari satu role; assigned_by dan assigned_at dicatat.
•           FR-18 Role ADMIN wajib punya minimal satu site_id; ADMIN_WORKER dan WORKER wajib punya tepat satu vendor_id.
•           FR-19 Admin Worker hanya bisa membuat dan reset password user ber-role WORKER di vendornya sendiri.
•           FR-20 User tidak dihapus permanen, hanya dinonaktifkan, agar jejak audit tetap utuh.
5.5 Middleware otorisasi
•           FR-21 Middleware Authenticate memvalidasi tanda tangan dan masa berlaku JWT, lalu memuat user aktif.
•           FR-22 Middleware RequireRole menolak request dengan role yang tidak diizinkan endpoint (HTTP 403).
•           FR-23 Middleware Scope menyisipkan daftar site_id/vendor_id yang diizinkan ke context; repository wajib memfilter query dengan scope ini.
•           FR-24 Akses ke resource di luar scope mengembalikan HTTP 404 (bukan 403) agar keberadaan data lain tidak bocor.
•           FR-25 Setiap perubahan user, role, scope, dan password dicatat di audit_logs dengan data sebelum/sesudah (tanpa password atau token).
6. Alur Login & Refresh Token

Alur login, request terproteksi, dan refresh token
Role dibawa di access token, tetapi scope selalu dibaca ulang dari database, sehingga pencabutan scope langsung berlaku tanpa menunggu token kedaluwarsa.
7. Model Data
Modul ini memakai enam tabel milik auth dan dua tabel referensi minimal (sites, vendors) yang detailnya ada di PRD Master Data.
Tabel
Fungsi
Kolom kunci
users
Akun login
username (unik), password_hash, email (opsional), must_change_password, failed_login_count, locked_until, is_active, last_login_at
roles
Daftar role (seed)
code: SUPER_ADMIN, ADMIN, ADMIN_WORKER, WORKER
user_roles
Relasi user ↔ role
PK (user_id, role_id), assigned_by, assigned_at
user_scopes
Batas data per user
site_id (scope Admin), vendor_id (scope Admin Worker/Worker)
refresh_tokens
Sesi login
token_hash (unik), expires_at, revoked_at, user_agent, ip_address
audit_logs
Jejak aktivitas
action, entity, entity_id, before_data, after_data, ip_address
sites
Lokasi/fasilitas (minimal)
code, name, site_type
vendors
Perusahaan vendor (minimal)
code, name

Aturan data
•           Constraint CHECK pada user_scopes: minimal salah satu dari site_id atau vendor_id terisi.
•           Index pada refresh_tokens(user_id), user_scopes(user_id), dan audit_logs(user_id, created_at).
•           Kolom before_data/after_data tidak boleh berisi password_hash atau token.
Relasi & nullability (DBML)
Relasi wajib memakai > dan kolom FK-nya NOT NULL; relasi opsional memakai >? dan kolom FK-nya boleh kosong.
Kolom FK
Wajib?
Alasan
user_roles.user_id, user_roles.role_id
Wajib
Bagian dari primary key
user_scopes.user_id
Wajib
Scope selalu milik satu user
refresh_tokens.user_id
Wajib
Sesi selalu milik satu user
user_roles.assigned_by
Opsional
Seed awal Super Admin tidak punya pemberi role
user_scopes.site_id
Opsional
Hanya terisi untuk role Admin
user_scopes.vendor_id
Opsional
Hanya terisi untuk Admin Worker/Worker
audit_logs.user_id
Opsional
Login gagal dengan username tidak dikenal tidak punya user

Skema DBML lengkap ada di Lampiran A.
8. API Endpoints
Semua endpoint di bawah prefix /api/v1. Endpoint selain login dan refresh wajib header Authorization: Bearer <access_token>.
Method
Endpoint
Role
Keterangan
POST
/auth/login
Publik
Username + password → access & refresh token
POST
/auth/refresh
Publik (refresh token)
Rotasi refresh token, access token baru
POST
/auth/logout
Semua
Cabut sesi saat ini
POST
/auth/logout-all
Semua
Cabut semua sesi milik sendiri
GET
/auth/me
Semua
Profil, role, dan scope user login
PUT
/auth/password
Semua
Ganti password sendiri
GET
/users
Super Admin, Admin Worker
Daftar user (Admin Worker: worker vendornya saja)
POST
/users
Super Admin, Admin Worker
Buat user (Admin Worker: hanya WORKER)
GET
/users/{id}
Super Admin, Admin Worker
Detail user dalam scope
PATCH
/users/{id}
Super Admin, Admin Worker
Ubah profil, aktif/nonaktif
POST
/users/{id}/reset-password
Super Admin, Admin Worker
Password sementara, wajib ganti saat login
PUT
/users/{id}/roles
Super Admin
Set role user
PUT
/users/{id}/scopes
Super Admin
Set site/vendor scope
DELETE
/users/{id}/sessions
Super Admin
Cabut semua sesi user
GET
/roles
Super Admin
Daftar role
GET
/audit-logs
Super Admin
Filter user, action, entity, tanggal

Format error: { "error": { "code": "AUTH_INVALID_CREDENTIALS", "message": "..." } } dengan status 400, 401, 403, 404, 423 (akun terkunci), atau 429 (rate limit).
9. Kebutuhan Non-Fungsional & Keamanan
Aspek
Kebutuhan
Transport
Semua trafik lewat HTTPS; cookie (jika dipakai) Secure, HttpOnly, SameSite=Strict
Secret JWT
Disimpan di environment/secret manager, bukan di repo; mendukung rotasi kunci via kid
Rate limit
Login dan refresh dibatasi per IP dan per username (usulan 10 request/menit)
Penyimpanan token di klien
Web: refresh token di cookie HttpOnly; PWA worker: sama, access token di memori
Performa
Validasi token + muat scope < 50 ms per request (p95)
Audit
Audit log tidak bisa diubah/dihapus lewat API; retensi mengikuti kebijakan site
Logging
Log aplikasi tidak boleh mencetak password, hash, atau token
Ketersediaan
Kegagalan auth tidak membuka akses (fail closed)

10. Acceptance Criteria
Modul dianggap selesai bila semua test di bawah lulus sebagai automated test di CI.
Autentikasi
•           ☐ Login dengan kredensial benar mengembalikan access + refresh token dan tercatat di audit log.
•           ☐ Login gagal 5 kali mengunci akun 15 menit (HTTP 423).
•           ☐ User nonaktif tidak bisa login dan refresh tokennya ditolak.
•           ☐ Access token kedaluwarsa ditolak (HTTP 401); refresh berhasil menerbitkan token baru.
•           ☐ Refresh token lama yang dipakai ulang setelah rotasi mencabut semua sesi user.
•           ☐ Logout membuat refresh token tidak bisa dipakai lagi.
Isolasi akses (wajib sejak Sprint 1)
ID
Skenario
Hasil yang diharapkan
ISO-01
Admin Worker Vendor A membuka data milik Vendor B
404, tidak ada data bocor
ISO-02
Admin Worker Vendor A membuat user ber-role ADMIN
403
ISO-03
Admin Worker Vendor A reset password worker Vendor B
404
ISO-04
Admin Site X membuka data Site Y
404
ISO-05
Admin Site X melihat data semua vendor di Site X
200, hanya vendor di Site X
ISO-06
Worker membuka data worker lain
404
ISO-07
Worker memanggil endpoint /users
403
ISO-08
Scope user dicabut saat sesi aktif
Request berikutnya langsung ditolak (scope dibaca dari DB)
ISO-09
Token dimodifikasi (role diubah manual)
401, tanda tangan tidak valid

11. Open Questions
•           ☐ Durasi access token (usulan 15 menit) dan refresh token (usulan 7 hari) — perlu lebih panjang untuk worker shift malam?
•           ☐ Batas gagal login dan durasi kunci (usulan 5 kali, 15 menit).
•           ☐ Lupa password: cukup reset oleh Admin Worker/Super Admin, atau perlu jalur mandiri (mis. WhatsApp)?
•           ☐ Format username: bebas, atau mengikuti nomor pegawai dari vendor?
•           ☐ Apakah Admin boleh mengelola user di site-nya, atau tetap hanya Super Admin?
•           ☐ Retensi audit log (mis. 1 tahun, 3 tahun).
•           ☐ Perlu 2FA untuk Super Admin?
Lampiran A. Skema DBML (dbdiagram.io)
Table users {
   id uuid [pk]
   username varchar(30) [unique, not null, note: 'login, lowercase, tidak bisa diubah']
   password_hash text [not null]
   full_name varchar(150)
   email varchar(150) [unique, note: 'opsional, untuk kontak/notifikasi']
   phone varchar(30)
   must_change_password boolean [default: true]
   failed_login_count int [default: 0]
   locked_until timestamptz
   is_active boolean [default: true]
   last_login_at timestamptz
   created_at timestamptz
   updated_at timestamptz
 }

 Table roles {
   id smallint [pk]
   code varchar(30) [unique, not null, note: 'SUPER_ADMIN | ADMIN | ADMIN_WORKER | WORKER']
   name varchar(50) [not null]
 }

 Table user_roles {
   user_id uuid [not null, ref: > users.id]
   role_id smallint [not null, ref: > roles.id]
   assigned_at timestamptz
   assigned_by uuid [ref: >? users.id]
   indexes {
 	(user_id, role_id) [pk]
   }
 }

 // SUPER_ADMIN tidak butuh baris scope (akses semua)
 Table user_scopes {
   id uuid [pk]
   user_id uuid [not null, ref: > users.id]
   site_id uuid [ref: >? sites.id, note: 'scope ADMIN']
   vendor_id uuid [ref: >? vendors.id, note: 'scope ADMIN_WORKER / WORKER']
   created_at timestamptz
 }

 Table refresh_tokens {
   id uuid [pk]
   user_id uuid [not null, ref: > users.id]
   token_hash text [unique, not null]
   expires_at timestamptz [not null]
   revoked_at timestamptz
   user_agent text
   ip_address inet
   created_at timestamptz
 }

 Table audit_logs {
   id bigserial [pk]
   user_id uuid [ref: >? users.id, note: 'kosong untuk login gagal']
   action varchar(30) [note: 'LOGIN | LOGOUT | LOGIN_FAILED | CREATE | UPDATE | DELETE']
   entity varchar(50)
   entity_id uuid
   before_data jsonb
   after_data jsonb
   ip_address inet
   created_at timestamptz
 }

 Table sites {
   id uuid [pk]
   code varchar(20) [unique, not null]
   name varchar(150) [not null, note: 'mis. PLTU Paiton']
   site_type varchar(30) [note: 'mis. PLTU, pabrik, gedung']
 }

 Table vendors {
   id uuid [pk]
   code varchar(20) [unique, not null]
   name varchar(150) [not null]
 }


