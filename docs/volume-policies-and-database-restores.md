# Volume protection and restore into a new database

`user.volume_policy.list`, `get`, `create`, `update`, `delete`, `attach`, `detach`, and `reset_failures` manage standalone volume protection policies. The list tool follows every pagination page. Get returns policy fields, attached volumes, attachable volumes and the latest 50 runs.

Create a daily policy:

```json
{"name":"Nightly protection","frequency":"daily","schedule_time":"02:30","timezone":"Europe/Amsterdam","backup_enabled":true,"snapshot_enabled":true,"retention_mode":"gfs","keep_last":2,"keep_daily":7,"keep_weekly":4}
```

Times are wall-clock times in the named IANA timezone and follow daylight saving time. Omitted or null timezone follows the account owner's effective timezone. Count retention requires retention_count (1..365); GFS requires at least one positive keep rule.

Partially update a policy with `user.volume_policy.update`:

```json
{"id":"POLICY_UUID","timezone":null,"snapshot_enabled":false,"keep_last":0}
```

Omitted fields stay unchanged. Explicit null clears timezone, schedule_day or a keep rule. Zero disables a keep rule. Null retention_mode or retention_count preserves the current value. At least one of backups and snapshots must remain enabled. Weekly frequency requires schedule_day (0 Sunday through 6 Saturday); switching to daily clears the day.

Attach and detach take `policy_id` and `volume_id`. A volume can belong to one policy; Proxmox per-volume protection is unsupported. Detaching keeps existing artifacts. Delete takes `id` and `confirm:true`, detaches all members and keeps existing artifacts. Reset failures takes `id` and resumes a paused policy.

`user.managed_database.restore_to_new` creates a new database at the source location with the same engine and major version:

```json
{"id":"SOURCE_DATABASE_UUID","backup_id":"BACKUP_UUID","name":"orders-restored"}
```

For a point in time, replace backup_id with target_time, for example `2026-10-28T14:30:00Z`. Supply exactly one. Optional db_plan_id overrides the source plan. Optional vpc_id overrides the source VPC; when supplying it, also supply vpc_subnet_id. The target needs a VPC, enough disk, and a usable backup chain.

Acceptance returns managed_database_id, task_id and status restoring. Follow the new database with `user.managed_database.get`; billing begins when it activates. The source stays unchanged and its backup policy is not copied.

A 503 deploy_dispatch_unconfirmed is an error with the new database and task IDs: they are retained and reconciliation continues. Do not repeat the restore request. Follow those IDs. A transport error may also leave work behind; inspect databases and tasks before retrying. The tool sends each restore request once and does not wait for activation.
