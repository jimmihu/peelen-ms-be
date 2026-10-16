-- =====================================================
-- 009: Revisi laporan task oleh worker
--
-- Worker boleh mengajukan revisi laporan yang sudah COMPLETED berkali-kali
-- (selama jendela upload belum tutup). Setiap revisi wajib foto evidence
-- before & after yang baru. Versi sebelumnya disimpan di
-- pipeline_report_revisions; pipeline_report_details selalu versi terbaru.
--
--   revision_no 0 = laporan asli, 1 = revisi pertama, dst.
--   pipeline_reports.revision_count = jumlah revisi = nomor versi terbaru.
-- =====================================================
BEGIN;

ALTER TABLE pipeline_reports
    ADD COLUMN IF NOT EXISTS revision_count integer NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS pipeline_report_revisions (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    pipeline_report_id uuid NOT NULL REFERENCES pipeline_reports(id) ON DELETE RESTRICT,
    -- Versi yang digantikan (arsip): 0 = laporan asli
    revision_no        integer NOT NULL CHECK (revision_no >= 0),
    evidence_before    text, -- object-storage key
    evidence_after     text, -- object-storage key
    open_date          timestamptz,
    closed_date        timestamptz,
    note_category      varchar(30),
    notes              text,
    -- Pengaju revisi yang menggantikan versi ini, beserta alasannya
    revised_by         uuid REFERENCES users(id) ON DELETE SET NULL,
    reason             text,
    created_at         timestamptz NOT NULL DEFAULT now(),
    UNIQUE (pipeline_report_id, revision_no)
);

COMMIT;
