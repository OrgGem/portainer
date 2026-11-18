# Authentication and activity logs

The administrator-only User Activity page reads two different sources. Neither
source is a complete audit trail for every API request.

Authentication outcomes are appended as JSONL in
`<datastore>/logs/authentication/YYYY-MM-DD.jsonl`, using UTC calendar days.
The server keeps the current UTC day and the preceding 29 days, prunes at
startup and each UTC midnight, and creates new files with owner-only
permissions. Include this directory in backups if these records are needed.
Logging begins when this version starts; earlier login history is not inferred.

Login events include unsuccessful requests. Rate-limited requests are sampled
globally across all clients and both login endpoints at most once per 10 seconds.
The first rejection is recorded immediately; a later sample carries the count of
rejections suppressed since the preceding sample. A sample's origin is only that
sampled request's origin, not all suppressed origins. If there is no subsequent
rejection or the process restarts, a pending count remains unavailable or is
lost. The UI warns when sampled entries exist and CSV includes sampling fields;
these entries are not a complete per-request audit trail. A logout is
recorded only when the existing logout handler recognizes an authenticated
cookie. A method or username may be unknown. Addresses use the same trusted
proxy configuration as the login rate limiter; forwarding headers are not
trusted if no trusted proxies are configured. Passwords, request bodies,
tokens, cookies, OAuth authorization codes and provider errors are not stored
in authentication records.

Authentication continues if log storage fails. The JSON response reports an
unavailable or partial source if files cannot be read, a damaged line is found,
or a storage operation has failed during the current process lifetime. Valid
lines remain readable after damaged lines. CSV export refuses an unavailable
or partial source instead of silently exporting incomplete data. A failure that
prevents writing cannot itself be durably recorded on the failed storage, so
the process-lifetime warning cannot survive every restart.

Activity is a projection of data already present in the database:

* Configuration events are limited by the existing 200-event history per
  stack and use the current username associated with the recorded user ID.
* Deployment events represent only the current deployment lifecycle; a new
  deployment resets the lifecycle. They have no attributed actor. Error text is
  replaced by a standard status summary.
* Creation and last-update metadata provide the recorded actor and timestamp,
  without implying whether an operation succeeded. Missing timestamps do not
  produce entries.

Deleting a stack removes its projected history. Stack contents, environment
variables, Git credentials and raw deployment errors are not included. CSV
exports use the requested filters and ordering, include all matching rows, use
UTF-8 with a BOM and escape spreadsheet formula prefixes.
