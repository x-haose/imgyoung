-- name: CreateImportJob :exec
INSERT INTO import_jobs (id, user_id, mode, status, seeds, ttl_seconds, exif_policy, referer)
VALUES (@id, @user_id, @mode, @status, @seeds, sqlc.narg(ttl_seconds), @exif_policy, @referer);

-- name: GetImportJob :one
SELECT id, user_id, mode, status, seeds, ttl_seconds, exif_policy, referer,
       scan_pages_found, scan_pages_done, scan_report, error_code, created_at, finished_at
FROM import_jobs
WHERE id = @id;

-- name: ListImportJobs :many
SELECT id, user_id, mode, status, seeds, ttl_seconds, exif_policy, referer,
       scan_pages_found, scan_pages_done, scan_report, error_code, created_at, finished_at
FROM import_jobs
WHERE (CAST(@user_id AS TEXT) = '' OR user_id = CAST(@user_id AS TEXT)) AND status <> 'creating'
ORDER BY created_at DESC, rowid DESC
LIMIT @page_limit OFFSET @page_offset;

-- name: CountImportJobs :one
SELECT COUNT(*)
FROM import_jobs
WHERE (CAST(@user_id AS TEXT) = '' OR user_id = CAST(@user_id AS TEXT)) AND status <> 'creating';

-- name: NextRunnableImportJob :one
SELECT id, user_id, mode, status, seeds, ttl_seconds, exif_policy, referer,
       scan_pages_found, scan_pages_done, scan_report, error_code, created_at, finished_at
FROM import_jobs
WHERE status IN ('scanning', 'importing')
ORDER BY created_at, rowid
LIMIT 1;

-- name: TransitionImportJob :execrows
UPDATE import_jobs
SET status = @to_status,
    error_code = @error_code,
    finished_at = sqlc.narg(finished_at)
WHERE id = @id AND status = @from_status;

-- name: CancelImportJob :execrows
UPDATE import_jobs
SET status = 'cancelled', finished_at = @finished_at
WHERE id = @id AND status IN ('scanning', 'ready', 'importing');

-- name: ResumeImportJob :execrows
UPDATE import_jobs
SET status = 'importing', error_code = '', finished_at = NULL
WHERE id = @id AND status IN ('done', 'aborted', 'cancelled');

-- name: DeleteImportJob :execrows
DELETE FROM import_jobs
WHERE id = @id AND status IN ('ready', 'done', 'cancelled', 'aborted');

-- name: DeleteCreatingImportJob :execrows
DELETE FROM import_jobs
WHERE id = @id AND status = 'creating';

-- name: DeleteStaleCreatingImportJobs :execrows
DELETE FROM import_jobs
WHERE status = 'creating';

-- name: SetImportScanProgress :exec
UPDATE import_jobs
SET scan_pages_found = @pages_found, scan_pages_done = @pages_done
WHERE id = @id;

-- name: FinishImportScan :execrows
UPDATE import_jobs
SET status = 'ready',
    scan_pages_found = @pages_found,
    scan_pages_done = @pages_done,
    scan_report = @scan_report
WHERE id = @id AND status IN ('scanning', 'creating');

-- name: ResetImportScan :exec
UPDATE import_jobs
SET scan_pages_found = 0, scan_pages_done = 0, scan_report = NULL
WHERE id = @id;

-- name: ClearImportCandidates :exec
DELETE FROM import_items
WHERE job_id = @job_id AND state = 'candidate';

-- name: UpsertImportItem :one
INSERT INTO import_items (job_id, url, host, state)
VALUES (@job_id, @url, @host, @state)
ON CONFLICT (job_id, url) DO UPDATE SET job_id = excluded.job_id
RETURNING id;

-- name: AddImportItemSource :exec
INSERT OR IGNORE INTO import_item_sources (item_id, source, raw)
VALUES (@item_id, @source, @raw);

-- name: CountImportItemsByState :many
SELECT state, COUNT(*) AS item_count
FROM import_items
WHERE job_id = @job_id
GROUP BY state;

-- name: ListImportHosts :many
SELECT host, COUNT(*) AS item_count
FROM import_items
WHERE job_id = @job_id AND state = 'candidate'
GROUP BY host
ORDER BY item_count DESC, host;

-- name: ListImportHostSamples :many
SELECT host, url
FROM (
    SELECT host, url, ROW_NUMBER() OVER (PARTITION BY host ORDER BY id) AS rn
    FROM import_items
    WHERE job_id = @job_id AND state = 'candidate'
)
WHERE rn <= 3
ORDER BY host, rn;

-- name: ListImportItems :many
SELECT i.id, i.url, i.host, i.state, i.image_id, i.error_code, i.error_detail,
       (SELECT COUNT(*) FROM import_item_sources s WHERE s.item_id = i.id) AS source_count,
       CAST((SELECT json_group_array(source)
             FROM (SELECT source FROM import_item_sources s WHERE s.item_id = i.id ORDER BY s.id LIMIT 5))
            AS TEXT) AS sources
FROM import_items i
WHERE i.job_id = @job_id
  AND (CAST(@state AS TEXT) = '' OR i.state = CAST(@state AS TEXT))
  AND (CAST(@host AS TEXT) = '' OR i.host = CAST(@host AS TEXT))
ORDER BY i.id
LIMIT @page_limit OFFSET @page_offset;

-- name: CountImportItems :one
SELECT COUNT(*)
FROM import_items
WHERE job_id = @job_id
  AND (CAST(@state AS TEXT) = '' OR state = CAST(@state AS TEXT))
  AND (CAST(@host AS TEXT) = '' OR host = CAST(@host AS TEXT));

-- name: ListPendingImportItems :many
SELECT i.id, i.url,
       CAST(COALESCE((SELECT source FROM import_item_sources s WHERE s.item_id = i.id ORDER BY s.id LIMIT 1), '') AS TEXT) AS first_source
FROM import_items i
WHERE i.job_id = @job_id AND i.state = 'pending' AND i.id > @after_id
ORDER BY i.id
LIMIT @page_limit;

-- name: MarkImportItemDone :exec
UPDATE import_items
SET state = 'done', image_id = @image_id, error_code = '', error_detail = ''
WHERE id = @id;

-- name: MarkImportItemFailed :exec
UPDATE import_items
SET state = 'failed', error_code = @error_code, error_detail = @error_detail
WHERE id = @id;

-- name: ConfirmImportItems :execrows
UPDATE import_items
SET state = 'pending'
WHERE job_id = @job_id AND state = 'candidate' AND host IN (sqlc.slice('hosts'));

-- name: SkipImportCandidates :exec
UPDATE import_items
SET state = 'skipped'
WHERE job_id = @job_id AND state = 'candidate';

-- name: RequeueFailedImportItems :execrows
UPDATE import_items
SET state = 'pending', error_code = '', error_detail = ''
WHERE job_id = @job_id AND state = 'failed';

-- name: CountPendingImportItems :one
SELECT COUNT(*)
FROM import_items
WHERE job_id = @job_id AND state = 'pending';

-- name: ListImportMappingItems :many
SELECT id, url, image_id
FROM import_items
WHERE job_id = @job_id AND state = 'done' AND image_id IS NOT NULL
ORDER BY id;

-- name: ListImportMappingReferences :many
SELECT s.item_id, s.source, s.raw
FROM import_item_sources s
JOIN import_items i ON i.id = s.item_id
WHERE i.job_id = @job_id AND i.state = 'done' AND i.image_id IS NOT NULL
ORDER BY s.item_id, s.id;
