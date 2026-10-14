# Pulse: Detailed Implementation Checklist

## How to use this document

This is the execution checklist for Pulse. The product scope and
technical decisions are frozen for v1. Work in order, finish the
acceptance criteria, commit, and move on. Do not add features just
because they sound interesting.

**Working style** 1. Work on one numbered step at a time. 2. Complete
its checkboxes in order. A substep is a concrete action, not a research
rabbit hole. 3. Ask for an explanation when a concept is new. Understand
the purpose and the important lines, but do not try to memorize every
library detail. 4. Run the listed checks at the end of a meaningful
batch, not after every line. 5. If a known issue does not block the
current acceptance criteria, record it and continue. 6. Keep changes
small enough to debug, but large enough to finish features quickly. 7.
Commit when the step's acceptance criteria pass.

## Product scope

Pulse lets registered users monitor HTTP/HTTPS URLs. It checks them on a
schedule, stores results, shows current status and history, opens
incidents after sustained failures, and sends webhook alerts.

### Required for v1

-   Registration and login.
-   Authenticated monitor create, list, view, update, enable/disable,
    and delete.
-   Validated check intervals.
-   Separate API and worker processes.
-   PostgreSQL persistence and durable scheduled jobs.
-   Check history and latest status.
-   Incident opening and recovery.
-   Webhook alerts.
-   A simple usable dashboard.
-   Tests, logs, metrics, health checks, Docker Compose, CI, deployment
    instructions, and README.

### Explicitly not in v1

Kubernetes, Kafka, service meshes, multi-region deployment, billing,
teams, OAuth, mobile apps, AI summaries, plugin systems, or a
microservices rewrite.

## Fixed technical decisions

  -----------------------------------------------------------------------
  Area                                Decision
  ----------------------------------- -----------------------------------
  Language                            Go

  HTTP API                            Gin

  Database                            PostgreSQL

  Ordinary database operations        GORM with `gorm.io/gorm` and
                                      `gorm.io/driver/postgres`

  Schema migrations                   Goose with versioned SQL files

  Configuration                       Environment variables with
                                      validation

  Logging                             Go `slog`

  API contract                        OpenAPI after routes stabilize

  Local environment                   Docker Compose

  Tests                               Go `testing`, including PostgreSQL
                                      integration tests

  Background work                     Separate Go worker using PostgreSQL
                                      as a durable queue

  Metrics                             Prometheus-compatible

  CI                                  GitHub Actions or equivalent

  Frontend                            Server-rendered templates or a
                                      small separate frontend; do not let
                                      it delay the backend
  -----------------------------------------------------------------------

Goose owns schema changes. Do not replace reviewed SQL migrations with
GORM `AutoMigrate`. Use raw, parameterized SQL for PostgreSQL-specific
locking when needed.

## Target architecture

``` text
Browser / API client
        |
        v
    Gin API ---------> PostgreSQL <--------- Pulse worker
    auth + CRUD        users                  scheduler/dispatcher
                       monitors               bounded check workers
                       check history           incidents and alerts
                       jobs
        |                                         |
        v                                         v
 health, logs, metrics                       monitored URLs
```

The checker performs one HTTP probe and returns a result. It must not
know about Gin, authentication, or database storage. The API does not
perform scheduled checks in user-facing requests.

------------------------------------------------------------------------

# Phase A: Stabilize the existing checker

## Step 1: Establish a reproducible baseline

**Goal:** Know what exists and make it easy to run again.

### Substeps

-   [ ] From the repository root, inspect the directory tree and
    identify the Go module directory.
-   [ ] Record the Go version with `go version`.
-   [ ] From the module directory, record the module path and direct
    dependencies using `go.mod` and `go list -m`.
-   [ ] Run the current program using the documented working directory
    and command.
-   [ ] Inspect recent SQLite rows using the existing read function or
    database viewer.
-   [ ] Run `go test ./...`.
-   [ ] Run `go vet ./...`.
-   [ ] Write down existing failures instead of disguising them as new
    regressions.
-   [ ] Add a short README section explaining what the current program
    does, how to run it, and what it does not do.
-   [ ] Commit the baseline.

**Verify:** Another run from the documented directory produces
understandable output. You can explain the order in which `main` opens
the database, migrates, checks URLs, saves results, and reads history.

**Known gaps to record:** one-shot execution, unused ticker, hardcoded
URLs, no users/API, and SQLite concurrent-write locking.

## Step 2: Make one HTTP check correct and testable

**Goal:** A single HTTP probe has clear behavior independent of
persistence and scheduling.

### Substeps

-   [ ] Keep a result struct with URL, check time, status
    classification, duration, HTTP status code, and error/failure
    reason.
-   [ ] Keep `checkURL` separate from database and API code.
-   [ ] Pass in a reusable `*http.Client` instead of creating a client
    for each URL.
-   [ ] Configure a finite client timeout.
-   [ ] Ensure response bodies are closed with `defer resp.Body.Close()`
    after a response is received.
-   [ ] Distinguish an HTTP response from a transport failure. For
    example, HTTP 500 is a response with a status code, not a network
    error.
-   [ ] Decide and document the initial classification rule: response
    codes below 400 are `UP`, response codes 400 or higher are `DOWN`.
-   [ ] Test HTTP 200 returns `UP`, no error, and status code 200.
-   [ ] Test HTTP 500 returns `DOWN`, no transport error, and status
    code 500.
-   [ ] Test an invalid URL returns `DOWN` with an error and status code
    0.
-   [ ] Test a slow local server with a shorter client timeout. Expect
    `DOWN`, a non-nil error, and status code 0.
-   [ ] Test a connection failure using a controlled local test setup.
    Expect `DOWN` and an error.
-   [ ] Run `go test -v ./cmd/pulse` and `go vet ./...`.

**Verify:** Tests do not depend on public websites. The checker has no
database or Gin dependency. HTTP status and transport errors remain
distinct.

**Stop condition:** Once these behaviors pass, do not keep refactoring
the checker unless a later requirement needs it.

## Step 3: Introduce bounded concurrency

**Goal:** Run multiple checks concurrently without creating unlimited
work or racing shared resources.

### Substeps

-   [ ] Keep a sequential version or a small test baseline so
    concurrency benefits can be compared.
-   [ ] Choose one limit mechanism: a fixed worker pool or a semaphore.
    Use a fixed worker pool for the first version.
-   [ ] Add a configuration value for maximum simultaneous checks, with
    a safe default and validation.
-   [ ] Create a jobs channel that carries target URLs or monitor work
    items.
-   [ ] Start no more workers than the configured limit.
-   [ ] Make each worker call the checker and return or persist a result
    through a clearly owned path.
-   [ ] Close the jobs channel when no more work is being submitted.
-   [ ] Wait for workers to finish before returning from a round.
-   [ ] Decide how cancellation affects queued and in-flight work. Use
    contexts where appropriate.
-   [ ] Do not let several goroutines write concurrently to SQLite
    without an intentional serialization strategy. Since PostgreSQL is
    planned next, keep any temporary SQLite workaround small.
-   [ ] Add a test that proves active checks never exceed the configured
    maximum.
-   [ ] Run `go test ./...`, `go test -race ./...`, and `go vet ./...`.

**Verify:** The worker count is bounded, all submitted work is either
completed or cancelled according to policy, and race detection passes
for tested paths.

**Concepts to understand:** goroutine, channel, `WaitGroup`, concurrency
versus parallelism, race, cancellation, and resource limits.

## Step 4: Replace SQLite with PostgreSQL and define persistence boundaries

**Goal:** Use PostgreSQL for durable product data and GORM for ordinary
database operations.

### Substeps

-   [ ] Add a Compose service for PostgreSQL with a named persistent
    volume and health check.
-   [ ] Add local environment configuration using placeholders, not
    committed secrets.
-   [ ] Add Goose and create the first versioned SQL migration.
-   [ ] Define only the initial tables needed now, starting with users
    and monitors when their implementation steps arrive. Add check
    history, jobs, and incidents in their respective steps rather than
    speculatively.
-   [ ] Add primary keys, foreign keys, `NOT NULL`, uniqueness
    constraints, timestamps, and indexes justified by expected queries.
-   [ ] Add GORM and its PostgreSQL driver.
-   [ ] Create a small database initialization function that reads
    configuration, opens GORM, and verifies connectivity.
-   [ ] Keep schema changes in Goose migrations. Do not use GORM
    `AutoMigrate` as the schema migration system.
-   [ ] Implement a basic create/read/update/delete flow through GORM
    and check every returned error.
-   [ ] Inspect generated SQL at least once and learn how to enable GORM
    debug logging locally.
-   [ ] Learn when a transaction is necessary for a multi-operation
    change.
-   [ ] Keep raw SQL available for PostgreSQL-specific operations such
    as job claiming with `FOR UPDATE SKIP LOCKED`.
-   [ ] Verify migrations from an empty database and document how to
    apply and roll back them.

**Verify:** PostgreSQL starts locally, Goose creates the schema, GORM
connects and performs tested CRUD, and the checker remains independent
of storage.

------------------------------------------------------------------------

# Phase B: Build the API

## Step 5: Create the Gin API skeleton

**Goal:** Start a controlled HTTP server with configuration and shutdown
behavior.

### Substeps

-   [ ] Create `cmd/pulse-api` as the API entry point.
-   [ ] Move reusable code into packages only when there is a real
    caller or clear responsibility.
-   [ ] Define configuration fields for port, database connection,
    environment, and relevant timeouts.
-   [ ] Load environment variables and validate required values at
    startup.
-   [ ] Fail with a clear startup error when required configuration is
    missing or invalid.
-   [ ] Create a Gin router and register a minimal route.
-   [ ] Add request logging, request IDs, and panic recovery.
-   [ ] Create an `http.Server` with explicit read, write, idle, and
    header timeouts where appropriate.
-   [ ] Add `GET /health/live` to report that the process is alive.
-   [ ] Add `GET /health/ready` to verify dependencies that the API
    actually requires.
-   [ ] Handle SIGINT/SIGTERM with a signal-aware context and graceful
    server shutdown.
-   [ ] Add a test for a health route and one invalid configuration
    case.

**Verify:** The API starts predictably, health endpoints have distinct
documented meanings, and shutdown is graceful.

## Step 6: Define the monitor API contract

**Goal:** Agree on request and response formats before building all
handlers.

### Substeps

-   [ ] List the routes needed for create, list, get, update,
    enable/disable, and delete.
-   [ ] Define request structs separately from database models.
-   [ ] Define response structs separately from database models.
-   [ ] Choose consistent JSON names and a consistent error response
    format.
-   [ ] Validate URL scheme and host. Accept only HTTP and HTTPS.
-   [ ] Validate interval minimum and maximum, input lengths, and
    required fields.
-   [ ] Decide pagination parameters and ordering for monitor history.
-   [ ] Choose status codes for success, invalid input, unauthenticated,
    forbidden/not found, conflict, and internal errors.
-   [ ] Write a minimal OpenAPI document or route contract.
-   [ ] Exercise sample requests with curl or an API client.

**Verify:** Each route has a documented method, path, input, output, and
error behavior. Invalid inputs are rejected consistently.

## Step 7: Add users and authentication

**Goal:** Identify callers safely so every monitor can have an owner.

### Substeps

-   [ ] Add a Goose migration for users with a unique normalized email
    or username, password hash, and timestamps.
-   [ ] Choose a maintained password-hashing library and safe
    parameters.
-   [ ] Implement registration, including duplicate-account handling.
-   [ ] Implement login without revealing whether an account exists in
    unnecessary detail.
-   [ ] Choose one authentication approach for v1: secure server-side
    sessions or short-lived access tokens with a deliberately designed
    refresh mechanism.
-   [ ] Store secrets only in environment configuration.
-   [ ] Add authentication middleware that validates credentials and
    attaches a user identity to request context.
-   [ ] Ensure passwords, tokens, and secrets are never logged.
-   [ ] Test successful registration/login, duplicate registration,
    wrong password, missing credentials, invalid credentials, and expiry
    where applicable.

**Verify:** Passwords are never stored in plaintext and protected
endpoints reject unauthenticated requests.

## Step 8: Implement monitor CRUD and ownership

**Goal:** Users can manage only their own monitors.

### Substeps

-   [ ] Add a Goose migration for monitors with owner ID, URL, name if
    needed, interval, enabled state, and timestamps.
-   [ ] Add the user foreign key and constraints.
-   [ ] Implement create using the authenticated user ID, not a user ID
    trusted from the request body.
-   [ ] Implement list scoped to the authenticated user.
-   [ ] Implement get scoped by both monitor ID and owner ID.
-   [ ] Implement update with validation.
-   [ ] Implement enable/disable.
-   [ ] Implement delete with an explicit policy for related jobs and
    history.
-   [ ] Handle not-found, invalid input, duplicate/conflict, and
    database errors.
-   [ ] Use a transaction if an operation changes multiple related
    records that must remain consistent.
-   [ ] Test every operation and test that User A cannot read, update,
    or delete User B's monitor.

**Verify:** CRUD works and ownership is enforced in database queries,
not merely hidden in the UI.

## Step 9: Store and query check history

**Goal:** Persist check results and expose reliable history and latest
status.

### Substeps

-   [ ] Add a migration for check results linked to a monitor.
-   [ ] Store check time, classification, latency, HTTP status code when
    available, and a bounded/sanitized failure reason.
-   [ ] Add an index matching monitor history queries, such as monitor
    ID plus check time.
-   [ ] Persist a result after a check completes.
-   [ ] Implement latest-status retrieval.
-   [ ] Implement a paginated history endpoint ordered by check time and
    a stable tie-breaker.
-   [ ] Enforce monitor ownership in history queries.
-   [ ] Define a basic retention policy for v1.
-   [ ] Test persistence across process restart, latest status,
    pagination, and cross-user isolation.

**Verify:** Results survive restarts, latest status is correct, and
history is paginated and access-controlled.

------------------------------------------------------------------------

# Phase C: Scheduling and workers

## Step 10: Specify scheduling rules

**Goal:** Decide scheduling behavior before implementing the scheduler.

### Substeps

-   [ ] Define the minimum and maximum permitted monitor interval.
-   [ ] Define the first scheduled time when a monitor is created or
    enabled.
-   [ ] Define how the next check time is calculated.
-   [ ] Define what happens when a monitor is disabled, deleted, or
    edited while work is pending.
-   [ ] Decide how missed intervals behave. For v1, skip accumulated
    missed intervals instead of creating an unlimited backlog.
-   [ ] Draw the path from monitor creation/update to a due job to a
    completed check.
-   [ ] Decide which scheduling state must survive process restart.
-   [ ] Write these rules down before coding.

**Verify:** Scheduling rules fit on one page and define restart behavior
and missed intervals.

## Step 11: Build a durable PostgreSQL job queue

**Goal:** Store scheduled work so it survives crashes and can be claimed
by multiple workers.

### Substeps

-   [ ] Add a jobs migration with job ID, monitor ID, scheduled time,
    state, attempt count, lease expiry, and timestamps as needed.
-   [ ] Define legal states and state transitions.
-   [ ] Add indexes for due and claimable jobs.
-   [ ] Implement safe job creation and rescheduling so repeated
    scheduler passes do not create unlimited duplicates.
-   [ ] Claim jobs in a short transaction using `FOR UPDATE SKIP LOCKED`
    where appropriate.
-   [ ] Use GORM for ordinary CRUD and parameterized raw SQL for the
    claim operation if clearer.
-   [ ] Commit the claim before making the HTTP request.
-   [ ] Never keep a database transaction open during network I/O.
-   [ ] Define lease expiry and recovery behavior.
-   [ ] Add integration tests with PostgreSQL for competing claims.

**Verify:** Two worker processes do not normally claim the same active
job, claims are recoverable, and no transaction is held during a probe.

## Step 12: Create the separate worker executable

**Goal:** Run API and background work independently from the same
repository.

### Substeps

-   [ ] Add `cmd/pulse-worker`.
-   [ ] Reuse shared checker, configuration, database, and domain code
    without importing Gin into the worker.
-   [ ] Implement a dispatcher that looks for due jobs.
-   [ ] Feed claimed work to a bounded pool.
-   [ ] Apply request contexts and timeouts to every probe.
-   [ ] Persist check results.
-   [ ] Update scheduling state and create the next job according to
    Step 10.
-   [ ] Add worker liveness/readiness behavior.
-   [ ] Handle graceful shutdown so no new work is claimed after
    shutdown begins.
-   [ ] Run API and worker in separate terminals and verify they use the
    same PostgreSQL database.

**Verify:** API and worker can run independently. Restarting the worker
does not erase queued work or history.

## Step 13: Handle crashes, leases, retries, and duplicate delivery

**Goal:** Recover work after failures and make repeated delivery safe.

### Substeps

-   [ ] Recover jobs whose lease expired after a worker crash.
-   [ ] Define retryable versus permanent failures.
-   [ ] Add bounded retry attempts and backoff.
-   [ ] Ensure repeated processing does not create duplicate logical
    check results or corrupt monitor state.
-   [ ] Document the idempotency key or guarded state transition used.
-   [ ] Decide what shutdown does with in-flight jobs: finish within a
    deadline, cancel, or let lease expiry recover them.
-   [ ] Test a worker stopping after claim, a probe timeout, a database
    interruption, and retry exhaustion.
-   [ ] Log failures with job ID and attempt number, without leaking
    secrets.

**Verify:** Expired claims recover, retries are bounded, duplicate
delivery is safe, and the behavior is documented. Do not claim
exactly-once execution.

## Step 14: Test multi-process coordination

**Goal:** Prove coordination works across independent processes.

### Substeps

-   [ ] Prepare an integration-test setup with PostgreSQL.
-   [ ] Insert more due jobs than one worker can handle at once.
-   [ ] Start two worker instances against the same database.
-   [ ] Verify jobs are claimed and recorded without duplicate active
    claims.
-   [ ] Stop a worker while it owns a job.
-   [ ] Wait for lease expiry and verify another worker recovers the
    job.
-   [ ] Test under database contention.
-   [ ] Record practical limits of the PostgreSQL queue.

**Verify:** A repeatable test demonstrates multi-worker processing and
crash recovery.

------------------------------------------------------------------------

# Phase D: Incidents, alerts, and dashboard

## Step 15: Implement incident state transitions

**Goal:** Convert repeated check observations into incident state.

### Substeps

-   [ ] Define the v1 policy, for example open after three consecutive
    failures and resolve after a successful check.
-   [ ] Add an incidents migration with monitor ID, start time,
    resolution time, and summary fields.
-   [ ] On each check result, evaluate the policy for that monitor.
-   [ ] Ensure repeated failures do not create multiple active incidents
    for the same monitor.
-   [ ] Resolve the active incident when the recovery condition is met.
-   [ ] Use transactions or guarded updates where concurrent workers
    could conflict.
-   [ ] Test failure threshold, continued failures, recovery, and a
    later new outage.

**Verify:** One outage produces one active incident, recovery resolves
it, and a later outage can open a new incident.

## Step 16: Send webhook alerts

**Goal:** Notify users about incident opening and recovery without
blocking monitoring.

### Substeps

-   [ ] Define how users configure and update a webhook destination.
-   [ ] Validate destination scheme and ownership.
-   [ ] Address server-side request forgery risks by restricting unsafe
    destinations and documenting remaining limits.
-   [ ] Create alert work independently from the check result
    persistence path.
-   [ ] Send incident-open and incident-resolved notifications with
    request timeouts.
-   [ ] Add bounded retries and record delivery outcome.
-   [ ] Sanitize logs and never log webhook secrets or tokens.
-   [ ] Test success, timeout, non-success response, retry, and
    exhausted retries.
-   [ ] Verify a failed webhook does not lose the check result or crash
    the worker.

**Verify:** Incident events generate notifications and failures remain
bounded and observable.

## Step 17: Build the dashboard

**Goal:** Make Pulse usable without curl.

### Substeps

-   [ ] Choose the simplest frontend approach that fits the current
    backend.
-   [ ] Build registration and login screens.
-   [ ] Build a monitor list showing name/URL, current status, last
    check, latency, and incident state.
-   [ ] Build the create/edit monitor form.
-   [ ] Build a monitor detail page with recent history.
-   [ ] Add loading, empty, validation-error, and API-error states.
-   [ ] Make status understandable without relying only on color.
-   [ ] Verify a narrow screen layout.
-   [ ] Use backend-confirmed state rather than inventing client-side
    status.

**Verify:** A user can register, create a monitor, see current status,
and inspect history through the UI.

------------------------------------------------------------------------

# Phase E: Production readiness and finish

## Step 18: Complete configuration and operational safety

**Goal:** Make local, test, and deployed configuration explicit.

### Substeps

-   [ ] List every configuration value used by API and worker.
-   [ ] Mark which values are required and which have safe defaults.
-   [ ] Validate types, ranges, URLs, and required secrets at startup.
-   [ ] Provide `.env.example` with placeholders only.
-   [ ] Set database pool limits and connection timeouts deliberately.
-   [ ] Configure inbound server and outbound checker timeouts.
-   [ ] Ensure sensitive values are not logged.
-   [ ] Document migration and startup ordering.
-   [ ] Test missing and malformed configuration.

**Verify:** Missing required settings fail clearly, secrets are not in
source control, and shutdown is predictable.

## Step 19: Add structured logs, metrics, and health

**Goal:** Diagnose behavior without attaching a debugger.

### Substeps

-   [ ] Use `slog` with structured fields.
-   [ ] Include useful request/job IDs, monitor ID, result, duration,
    retry count, and failure category.
-   [ ] Add counters for checks attempted, successful, and failed.
-   [ ] Add a latency histogram.
-   [ ] Add queue depth/age, retry, and alert-failure metrics.
-   [ ] Keep metric labels bounded. Never label by raw URL or arbitrary
    user ID.
-   [ ] Define API and worker liveness/readiness semantics.
-   [ ] Ensure diagnostics do not expose credentials or private data.
-   [ ] Verify a simulated failure is visible in logs and metrics.

**Verify:** You can follow a failed check and see aggregate failures and
queue backlog.

## Step 20: Complete the test suite

**Goal:** Protect the important product behavior against regression.

### Substeps

-   [ ] Unit-test checker classification, URL validation, interval
    rules, incident transitions, and retry policy.
-   [ ] Integration-test Goose migrations and GORM operations against
    PostgreSQL.
-   [ ] API-test authentication, CRUD, ownership, pagination, and
    errors.
-   [ ] Test concurrent job claiming and expired-lease recovery.
-   [ ] Ensure tests use local controlled servers, not uncontrolled
    public websites.
-   [ ] Add a regression test whenever a real bug is fixed.
-   [ ] Run `go test ./...`.
-   [ ] Run `go test -race ./...` where supported and useful.
-   [ ] Run `go vet ./...`.
-   [ ] Build API and worker.

**Verify:** Core rules are tested and all documented test commands work.

## Step 21: Containerize the complete stack

**Goal:** Start the project from a clean checkout using repeatable
commands.

### Substeps

-   [ ] Add a multi-stage Dockerfile or separate appropriate build
    targets for API and worker.
-   [ ] Add Compose services for PostgreSQL, API, and worker.
-   [ ] Configure database health checks and dependency readiness.
-   [ ] Persist PostgreSQL data in a named volume.
-   [ ] Run application processes as a non-root user where practical.
-   [ ] Document migration execution and safe local reset.
-   [ ] Verify environment values are passed without committing secrets.
-   [ ] Start the stack from a clean state and exercise one monitor.
-   [ ] Restart containers and verify data survives.

**Verify:** A clean checkout can start the stack with documented
commands, and API and worker remain separate processes.

## Step 22: Add CI and deploy

**Goal:** Build and run the product outside the development machine.

### Substeps

-   [ ] Add CI for formatting, tests, vet, and build.
-   [ ] Add race testing if supported and practical in CI.
-   [ ] Select a host that supports a persistent worker and PostgreSQL.
-   [ ] Confirm platform limits before committing to the host.
-   [ ] Configure secrets using the host's secret management.
-   [ ] Run migrations as a controlled deployment step.
-   [ ] Deploy API and worker.
-   [ ] Verify health endpoints and run a test monitor.
-   [ ] Restart the worker and verify recovery.
-   [ ] Document backup and rollback limitations.

**Verify:** CI runs on pushes and pull requests, and deployed monitoring
works end to end.

## Step 23: Write engineering documentation

**Goal:** Let another developer understand, run, and review Pulse.

### Substeps

-   [ ] Write a README covering purpose, features, prerequisites, local
    setup, configuration, migrations, tests, and deployment.
-   [ ] Add an architecture diagram.
-   [ ] Add a sequence diagram for a scheduled monitor check.
-   [ ] Document job states, leases, expiry, retries, and idempotency.
-   [ ] Document incident thresholds and alert behavior.
-   [ ] Explain trade-offs: PostgreSQL queue, bounded concurrency,
    retention, and limits at higher scale.
-   [ ] Include screenshots and a short demo.
-   [ ] List known limitations honestly.
-   [ ] Verify instructions from a clean checkout or a separate clone if
    practical.

**Verify:** Someone else can run the project and understand the full
monitor lifecycle without asking for missing steps.

## Step 24: Final acceptance test and freeze

**Goal:** Prove the promised v1 behavior and stop adding scope.

### Substeps

-   [ ] Start the stack from a clean environment.
-   [ ] Register two users.
-   [ ] Create monitors for both users.
-   [ ] Attempt cross-user reads and mutations and confirm they are
    rejected.
-   [ ] Run multiple workers and verify coordinated job claiming.
-   [ ] Use a controlled target that returns success, then repeated
    failures, then recovery.
-   [ ] Verify history and latest status.
-   [ ] Verify incident opening and resolution.
-   [ ] Verify webhook notifications and bounded failure behavior.
-   [ ] Restart a worker during claimed work and verify recovery.
-   [ ] Run tests and CI.
-   [ ] Verify deployed API/dashboard, logs, and metrics.
-   [ ] Finish README and demo.
-   [ ] Review Git status and ensure no secrets, database files, or
    build artifacts are committed.
-   [ ] Write a short retrospective and list future ideas separately.

**Verify:** All required behavior works, important security/recovery
tests pass, deployment is documented, and the repository is
reproducible.

**After Step 24, stop.** Fix real bugs and security issues, but put new
product ideas into a future-version list.

------------------------------------------------------------------------

# Progress tracker

-   [x] Step 1: Baseline and Git checkpoint, according to current
    working notes
-   [x] Step 2: Correct, testable checker
-   [ ] Step 3: Bounded concurrency
-   [ ] Step 4: PostgreSQL and persistence boundaries
-   [ ] Step 5: Gin API skeleton
-   [ ] Step 6: API contract
-   [ ] Step 7: Authentication
-   [ ] Step 8: Monitor CRUD and ownership
-   [ ] Step 9: Check history
-   [ ] Step 10: Scheduling rules
-   [ ] Step 11: Durable PostgreSQL job queue
-   [ ] Step 12: Separate worker executable
-   [ ] Step 13: Crash recovery, retries, and idempotency
-   [ ] Step 14: Multi-process coordination
-   [ ] Step 15: Incident transitions
-   [ ] Step 16: Webhook alerts
-   [ ] Step 17: Dashboard
-   [ ] Step 18: Configuration and operational safety
-   [ ] Step 19: Logs, metrics, and health
-   [ ] Step 20: Test suite
-   [ ] Step 21: Docker Compose
-   [ ] Step 22: CI and deployment
-   [ ] Step 23: Documentation
-   [ ] Step 24: Final acceptance and freeze

## Rules against endless perfection

1.  Acceptance criteria beat vague quality goals.
2.  Fix issues that block the current step. Record non-blocking issues
    and return when their planned step arrives.
3.  A refactor needs a concrete reason: a demonstrated bug, duplication,
    unclear ownership, or a required feature/test.
4.  No new infrastructure without a specific problem it solves.
5.  Complete a vertical slice rather than starting many abstractions.
6.  Keep learning notes short: concept, design choice, failure mode.
7.  Do not claim exactly-once job execution. Design for retries and
    idempotent effects.
8.  Freeze scope after Step 24.
