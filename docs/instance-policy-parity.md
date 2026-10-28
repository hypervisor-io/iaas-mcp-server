# Instance policies and capacity reasons

Create a GFS policy with `user.instance_backup_policy.create`:

```json
{"name":"nightly-gfs","full_backup_frequency":"daily","full_backup_time":"02:30","max_incremental_chain":0,"backup_device":"all","retention_mode":"gfs","keep_last":2,"keep_within_days":30,"keep_daily":7,"keep_weekly":4,"keep_monthly":6,"keep_yearly":1,"timezone":"Europe/Amsterdam"}
```

`retention_mode` is `count` (default) or `gfs`. Count creation requires `retention_count` from 1 to 365; GFS requires at least one positive keep rule. Keep maxima are 365, 3650, 365, 520, 120, and 30 respectively. Zero or null disables a keep rule; omitted fields keep their values during update. Get and list return all retention fields and the nullable timezone.

Use `user.instance_backup_policy.update` for partial edits:

```json
{"id":"policy-uuid","name":"renamed"}
```

This preserves all schedule and retention settings. Setting a zone:

```json
{"id":"policy-uuid","timezone":"Asia/Kolkata"}
```

Clearing the zone:

```json
{"id":"policy-uuid","timezone":null}
```

Omitting timezone preserves it; null clears it. Zone-only edits preserve the stored clock and weekday. To change the local schedule at the same time, include `full_backup_time` and, for weekly schedules, `full_backup_day`. Clocks use the explicit policy zone, otherwise the account owner timezone; the server converts legacy schedules to UTC storage. Invalid names, raw offsets and legacy aliases return a tool error containing `invalid_timezone`. An empty update object after the ID is a no-op.

`user.instance.create` preserves machine-readable capacity refusal reasons in its error, including `ipv6_not_available`, `storage_backend_not_offered`, and `no_capacity`. It does not continue to OS deployment after a refused create. Customer errors do not expose per-node details. Try another applicable location or plan, or inspect the refusal before retrying.
