#!/usr/bin/env bash
# Builds ddl/e2e_reset.sql: one SQL file (runnable in DBeaver or psql) that
# prepares a development database for E2E testing. Regenerate after changing
# 007_scoring.sql, seed_v3.sql, or seed_scoring.sql:  make e2e-sql
set -euo pipefail
cd "$(dirname "$0")/.."
{
  cat <<'HEADER'
-- =====================================================================
-- E2E RESET (development/testing only) - DIHASILKAN OTOMATIS, jangan diedit.
-- Sumber: 007_scoring.sql + seed_v3.sql + seed_scoring.sql (+ akhir 006).
-- Buat ulang dengan: make e2e-sql
--
-- PERINGATAN: menghapus SEMUA data di database ini lalu mengisi ulang.
--
-- Cara pakai:
--   DBeaver : buka file ini, jalankan sebagai script (Alt+X).
--             Kalau muncul "transaction aborted", jalankan ROLLBACK; dulu.
--   psql    : make e2e-reset   (sekaligus menjalankan scoring backfill)
--
-- Setelah selesai, jalankan server (make run). Job scoring langsung
-- menghitung fakta harian dan membekukan periode SLA/maturity yang sudah
-- selesai. Panduan akun & skenario: docs/E2E-TEST-DATA.md
-- =====================================================================

HEADER
  echo "-- ===== 1/4: tabel SLA & maturity (007_scoring.sql) ====="
  cat ddl/migrations/007_scoring.sql
  echo
  echo "-- ===== 2/4: reset & isi data (seed_v3.sql) ====="
  cat ddl/seed_v3.sql
  echo
  echo "-- ===== 3/4: aturan SLA & maturity (seed_scoring.sql) ====="
  cat ddl/seed_scoring.sql
  echo
  cat <<'FOOTER'
-- ===== 4/4: pipelines.site_id wajib (akhir 006_pipeline_site.sql) =====
-- Semua pipeline dari seed sudah punya site, jadi kolom bisa dijadikan NOT NULL.
BEGIN;
ALTER TABLE pipelines ALTER COLUMN site_id SET NOT NULL;
CREATE INDEX IF NOT EXISTS idx_pipelines_site_id ON pipelines(site_id);
COMMIT;

-- Cek cepat: site_id_nullable harus NO, jumlah data harus terisi.
SELECT (SELECT is_nullable FROM information_schema.columns
        WHERE table_name = 'pipelines' AND column_name = 'site_id') AS site_id_nullable,
       (SELECT count(*) FROM users)                     AS users,
       (SELECT count(*) FROM pipeline_item_assignments) AS assignments,
       (SELECT count(*) FROM pipeline_reports)          AS reports,
       (SELECT count(*) FROM score_rules)               AS rules;
FOOTER
} > ddl/e2e_reset.sql
echo "wrote ddl/e2e_reset.sql ($(wc -l < ddl/e2e_reset.sql) lines)"
