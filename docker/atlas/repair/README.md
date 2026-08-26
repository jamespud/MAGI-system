# S16 Event Sequence Repair Resumer

`magi_s16_event_sequence.sql` is a forward-only Atlas migration. If it was
interrupted mid-way (for example the server was rolled back, or the migration
returned after `ADD COLUMN seq` but before the unique key), simply re-running
the published migration fails on the first `ADD COLUMN seq`. This directory
provides an idempotent resume rather than editing the published file.

## When to run

Run `magi_s16_event_sequence_resume.sql` in an **offline maintenance window**
while every event writer is drained. It rebuilds `magi_event.seq` from
`(timestamp, id)` order, so it must **never** run after consumers have resumed
against a partially migrated schema.

## Procedure

1. Keep event writers drained (pause the backend).
2. Take/retain a backup (`mysqldump` of the `magi` schema).
3. Execute the resume script:

   ```bash
   mysql -h "$DB_HOST" -u "$DB_USER" -p "$DB_NAME" < docker/atlas/repair/magi_s16_event_sequence_resume.sql
   ```

4. Run the four S16 validation queries documented in `DEPLOYMENT.md`; every
   count must be zero.
5. Deploy only an S16-capable binary (one that always writes `seq`).
6. If any validation query returns a violation, restore from the backup and
   re-evaluate the window.

## Checkpoints it resumes

The script is safe to re-run from each of these states:

- pristine pre-S16 schema (no `seq` column),
- `seq` column added, cursor table created, no backfill,
- partial/complete backfill with a nullable `seq`,
- `seq` NOT NULL applied but the unique index missing,
- a wrong unique index sharing the name `uq_magi_event_case_seq`,
- a fully migrated schema (no-op; it validates and returns cleanly).

Helper procedures are dropped at both start and end, so an interrupted run can
be restarted without leftover state.
