# D221. The MaxCompute driver signs with version 4 and has no flavors

Status: Proposed. Ken decided item 1 on 2026-10-29.

1. The driver signs with signature version 4 only. The signing key is an HMAC with SHA-256 over
   `aliyun_v4` and the secret, the date, the region, `odps` and `aliyun_v4_request`, and the
   signature is the HMAC with SHA-1 of the canonical text under that key. The code is written
   here, and it copies none of the SDK (decided by Ken on 2026-10-29).
2. The driver follows no redirect. It sends the credentials to the host of the DSN only.
3. The driver has no flavor. The service is hosted, so `dbrun` cannot start it, and its tests
   run only where a person gives an account (the condition of D183 item 13).
4. A GET, and a request that made no connection, can go again after a reset or a handshake that
   timed out. A POST is never sent again after its connection was made, so a reset on a POST
   reaches the caller. The Jakarta endpoint resets many connections (recorded in
   `testdata/maxcompute/`).
5. `Connection: close` is not set by the driver. The recorder sets it, to keep the recording
   whole.

See `docs/MAXCOMPUTE.md`, sections "Requests" and "Flavors".
