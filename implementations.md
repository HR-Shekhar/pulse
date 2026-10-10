# Pulse - Finite Implementation Roadmap

**Purpose:** Build Pulse into a usable, deployable uptime-monitoring product while learning Go concurrency, backend engineering, system design, and the fundamentals of distributed systems.

**Completion rule:** This roadmap has **24 implementation steps**. Finish the acceptance criteria for each step, record the result, and move on. Do not add features or redesign completed steps unless a real bug, security issue, or unmet requirement demands it.

This is a learning guide, not a dump of finished code. You write the implementation. For each step, understand the design, implement the smallest useful slice, test it, and explain it in your own words.

---

## 1. Product definition and scope freeze

Pulse lets a registered user monitor HTTP/HTTPS URLs. It checks them on a schedule, stores results, shows current status and history, and creates incidents and alerts when a target becomes unhealthy.

### The finished product must support

- User registration and login.
- Authenticated monitor create, list, view, update, enable/disable, and delete operations.
- Configurable check interval within documented safe limits.
- Scheduled HTTP checks performed by a separate worker process.
- Persistent check history and a current status view.
- Incident creation on a sustained failure and recovery when the target becomes healthy again.
- User-configured webhook alerts (Discord-compatible webhook is sufficient).
- A usable dashboard.
- Local development through Docker Compose and a documented deployment path.
- Automated tests, structured logs, health/readiness endpoints, metrics, and a useful README.

### Explicitly out of scope for this version

Do **not** add Kubernetes, Kafka, service meshes, multi-region deployment, billing, teams/organizations, OAuth, mobile apps, AI-generated incident summaries, arbitrary plugins, or a microservices rewrite. These can become separate projects after this version is complete.

### Fixed technical direction

| Concern | Decision |
|---|---|
| Language | Go |
| HTTP API | Gin |
| Database | PostgreSQL |
| ORM (ordinary database operations) | GORM (`gorm.io/gorm`) |
| PostgreSQL dialect/driver for GORM | `gorm.io/driver/postgres` |
| Migrations | Goose, with versioned SQL migration files |
| Configuration | Environment variables with validation |
| Logging | Go `slog` with structured fields |
| API documentation | OpenAPI, added after routes stabilize |
| Local environment | Docker Compose |
| Tests | Go `testing`; integration tests against PostgreSQL |
| Frontend | Keep it simple: server-rendered HTML/templates or a small separate frontend. Do not let frontend work delay the backend. |
| Distributed work | A separate Go worker process using PostgreSQL as a durable job queue |
| Metrics | Prometheus-compatible metrics endpoint/library |
| CI | GitHub Actions or equivalent |

**Why no Redis or Kafka yet?** PostgreSQL can provide durable job storage and coordination for this project's first distributed-worker design. A separate API process and worker process already introduce meaningful real-world problems: competing consumers, leases, retries, duplicate execution, process crashes, and graceful shutdown. Add a broker only if you can identify a concrete limitation that the current design cannot meet.

---

## 2. Architecture to aim for

This is the target shape, not a requirement to create every package on day one.

```text
                    Browser / API Client
                            |
                            v
                    +----------------+
                    | Gin HTTP API   |
                    | auth + CRUD    |
                    +-------+--------+
                            |
                            v
                    +----------------+
                    | PostgreSQL     |
                    | users          |
                    | monitors       |
                    | check history  |
                    | jobs           |
                    | incidents      |
                    +---+---------+--+
                        ^         |
        claim due jobs  |         | query/update monitors
                        |         v
                 +------+---------+------+
                 | Pulse Worker          |
                 | scheduler/dispatcher  |
                 | bounded check workers |
                 | incident/alert logic  |
                 +-----------+-----------+
                             |
                             v
                     Monitored HTTP URLs

                 Worker/API -> logs, metrics, health
                 Alert processor -> configured webhook
```

### Responsibility boundaries

- **API:** validates requests, authenticates users, enforces ownership, and calls application operations. It does not perform scheduled URL checks inside user-facing requests.
- **Application/domain logic:** owns monitor rules, status transitions, incident rules, and validation that is not specific to HTTP.
- **Persistence:** PostgreSQL, GORM for ordinary database operations, explicit transactions, and Goose migrations. Use carefully scoped raw SQL for operations that need precise PostgreSQL behavior, such as concurrent job claiming.
- **Worker:** claims due work, executes checks with bounded concurrency, persists results, and triggers incident/alert processing.
- **Checker:** performs one HTTP probe and returns a structured result. It should not know about Gin or user authentication.
- **Alert sender:** sends notifications with timeouts and bounded retries. An alert failure must not crash monitoring.

Keep these as packages and processes with clear responsibilities. Do not turn each package into a network service without a reason.

---

## 3. How to work through the roadmap

For every step:

1. Read the goal and acceptance criteria.
2. Draw or write the data flow before coding.
3. Identify inputs, outputs, state, failure cases, and which component owns each decision.
4. Implement only what the step requires.
5. Run tests and deliberately trigger at least one failure.
6. Write a short note: **what I learned, what can fail, and why I chose this design**.
7. Commit the working step to Git.

Do not move on while the core behavior is broken. Do not wait for perfection once the acceptance criteria pass.

---

# Phase A - Understand and stabilize the existing checker

## Step 1 - Establish a reproducible baseline

**Goal:** Know exactly what the current code does before changing it.

**Do:**
- Record the current directory tree, Go version, module path, and dependencies.
- Run the current program and inspect the SQLite rows.
- Run `go test ./...` and `go vet ./...`; record existing failures rather than hiding them.
- Add a README note describing the current behavior and limitations.
- Commit the baseline.

**Think about:** Which parts are working behavior, and which are merely imported or declared but unused?

**Done when:**
- You can run the current program from a documented command.
- You can explain its execution order from `main`.
- You have recorded the known gaps: one-shot execution, ticker not consumed, hardcoded URLs, and no API/user model.

## Step 2 - Make one check correct and testable

**Goal:** Define the behavior of a single HTTP probe independently of storage and scheduling.

**Do:**
- Move the checker into a focused package when it makes the code easier to test.
- Define a result type containing target URL, start/finish time or check time, HTTP status code, UP/DOWN classification, latency, and a failure reason.
- Reuse an `http.Client` with a configured timeout instead of constructing one for every URL.
- Use request contexts and close response bodies.
- Document the rule for classifying HTTP status codes. A received `500` is an HTTP response, not a transport error.
- Test success, non-success HTTP status, invalid URL, timeout, and connection failure using `httptest` or controlled test servers.

**Think about:** What does “UP” mean? Is it “an HTTP response arrived,” or “the response meets the monitor's configured expectation”? Choose a clear initial rule.

**Done when:**
- The checker has no database or Gin dependency.
- Tests do not require access to public websites.
- Results distinguish HTTP status from network/timeout errors.

## Step 3 - Learn bounded concurrency

**Goal:** Understand goroutines, `WaitGroup`, races, and resource limits before building a long-running worker.

**Do:**
- Keep a small sequential baseline and compare it with concurrent checks.
- Introduce a configurable maximum number of checks running at once.
- Use a fixed worker pool or a semaphore-based limit; pick one approach and explain it.
- Ensure all work is joined before the round returns.
- Run `go test -race ./...`.
- Test with enough fake targets to prove the concurrency limit is respected.

**Think about:** A goroutine is cheap, but sockets, memory, remote services, database connections, and rate limits are not unlimited. Why is “one goroutine per monitor” not a sufficient capacity policy?

**Done when:**
- The number of active checks never exceeds the configured limit.
- The program waits for all work or cancels it according to the documented policy.
- You can explain the difference between concurrency and parallelism.
- Race detection passes for the tested paths.

## Step 4 - Define persistence boundaries and move to PostgreSQL

**Goal:** Replace SQLite with PostgreSQL and learn how GORM maps Go code to relational data without hiding the underlying database concepts.

**Do:**
- Add PostgreSQL to local Docker Compose.
- Create versioned Goose SQL migrations for the initial schema. Goose owns schema changes; do not use GORM AutoMigrate as a replacement for reviewed migrations.
- Use GORM (`gorm.io/gorm`) with its PostgreSQL driver (`gorm.io/driver/postgres`) for ordinary database operations.
- Introduce tables for users, monitors, check results, incidents, and jobs only when the relevant steps require them. Do not create speculative tables for future features.
- Use appropriate constraints, foreign keys, timestamps with time zones, and indexes based on actual queries.
- Keep database operations out of the checker.
- Learn to inspect the SQL generated by GORM, check returned errors, and use transactions where multiple changes must succeed or fail together.
- Keep raw SQL available for operations that need precise PostgreSQL behavior, such as job claiming with `FOR UPDATE SKIP LOCKED`. Start with GORM for ordinary CRUD; do not force every query through the ORM.
- Test migration up/down behavior where supported by the chosen migration strategy.

**Think about:** A schema is part of the product contract. Which queries must be fast: latest status, a monitor's recent history, due jobs, or a user's monitor list? GORM can make routine operations shorter, but it does not remove the need to understand SQL, indexes, constraints, or transactions.

**Done when:**
- PostgreSQL starts locally through documented commands.
- Goose migrations create the required schema from an empty database.
- GORM connects to PostgreSQL and can perform tested create/read/update/delete operations.
- The checker and storage layer have separate responsibilities.

---

# Phase B - Build the usable backend

## Step 5 - Create the Gin API skeleton

**Goal:** Start an HTTP API with explicit configuration and lifecycle.

**Do:**
- Organize the entry point under `cmd/pulse-api`.
- Load and validate configuration from environment variables.
- Configure Gin, request logging, request IDs, panic recovery, and sensible HTTP timeouts.
- Add `GET /health/live` and `GET /health/ready`. Readiness should check dependencies the API actually needs.
- Use `http.Server` rather than relying on a bare `gin.Engine.Run` for lifecycle control.
- Add a clean shutdown path using signal-aware contexts.

**Think about:** Liveness answers “is this process alive?” Readiness answers “should it receive traffic?” They are not the same question.

**Done when:**
- The API starts with valid configuration and fails clearly with invalid configuration.
- Health endpoints have documented semantics.
- SIGINT/SIGTERM triggers graceful shutdown.

## Step 6 - Define the monitor API contract

**Goal:** Decide what the API accepts and returns before adding persistence-heavy handlers.

**Do:**
- Define request/response structures for create, list, get, update, enable/disable, and delete monitor operations.
- Choose consistent JSON field names and error response format.
- Validate URL scheme, host, interval bounds, and input sizes.
- Define pagination for monitor history.
- Write a minimal OpenAPI specification or equivalent route documentation.

**Think about:** API contracts are consumed by clients. Avoid returning database models directly or exposing internal fields accidentally.

**Done when:**
- Each route, status code, request body, and response body is documented.
- Invalid inputs have consistent errors.
- You can demonstrate the contract using curl or an API client.

## Step 7 - Add users and secure authentication

**Goal:** Make monitor ownership real and enforce it at the API boundary and in database queries.

**Do:**
- Implement registration and login with a maintained password-hashing library and safe parameters.
- Never store plaintext passwords or log credentials.
- Choose one authentication model and document it. For a first API, short-lived access tokens with a carefully designed refresh mechanism are acceptable; secure server-side sessions are also valid.
- Keep secrets in environment configuration and provide a safe example file with placeholders.
- Add authentication middleware and an authenticated identity to request context.
- Test invalid credentials, expired/invalid tokens or sessions, and missing authentication.

**Think about:** Authentication establishes who the caller is. Authorization decides what that caller is allowed to access.

**Done when:**
- Passwords are securely hashed.
- Protected endpoints reject unauthenticated requests.
- Secrets are not committed or logged.
- Tests cover the main authentication failures.

## Step 8 - Implement monitor CRUD with ownership checks

**Goal:** Let each user manage only their own monitors.

**Do:**
- Implement create, list, get, update, enable/disable, and delete.
- Obtain the owner ID from authenticated identity, never from a trusted-looking request field.
- Scope reads and mutations by both monitor ID and owner ID.
- Add database constraints and handle not-found/conflict cases intentionally.
- Use transactions if an operation changes multiple related records.
- Add unit and PostgreSQL integration tests.

**Think about:** A random monitor ID is not authorization. Ask “does this record belong to this user?” on every user-scoped operation.

**Done when:**
- Two users can create monitors independently.
- One user cannot read, modify, or delete the other's monitor.
- CRUD and ownership tests pass against PostgreSQL.

## Step 9 - Store and query check history

**Goal:** Make history useful for the product, not just a debugging log.

**Do:**
- Store each check against a monitor ID.
- Record check time, status, latency, HTTP status code when available, and a bounded/sanitized failure reason.
- Implement a paginated history endpoint ordered by time and a latest-status query.
- Add indexes based on these query patterns.
- Decide a reasonable retention policy for the first version and document it.

**Think about:** Current state and historical events are different. History answers “what happened?” Current status answers “what is the latest known result?”

**Done when:**
- A monitor's history survives API and worker restarts.
- The latest result is correct.
- History pagination is stable and does not return another user's data.

---

# Phase C - Scheduling and distributed workers

## Step 10 - Specify the scheduling rules

**Goal:** Decide when work is due before implementing the scheduler.

**Do:**
- Define how a monitor's next check time is calculated.
- Define what happens when a monitor is disabled, deleted, or edited while a check is pending.
- Define the minimum and maximum check interval.
- Decide whether missed intervals are skipped or caught up. For this version, skip accumulated missed intervals rather than enqueueing an unlimited backlog.
- Draw the flow from a due monitor to a durable job.

**Think about:** A scheduler determines when work should happen; a worker performs the work. Mixing those responsibilities makes failure handling harder.

**Done when:**
- The scheduling policy fits on one page.
- The behavior for missed intervals and monitor edits is explicit.
- You can describe what survives a process restart.

## Step 11 - Build a durable PostgreSQL job queue

**Goal:** Store scheduled work so it survives process crashes and can be claimed by multiple worker processes.

**Do:**
- Add a jobs table with fields such as job ID, monitor ID, scheduled time, state, attempt count, lease/claim expiry, and creation time.
- Add the indexes needed to find due, claimable jobs.
- Claim jobs in a short transaction using PostgreSQL locking patterns such as `FOR UPDATE SKIP LOCKED`. Use GORM transactions where useful, and use parameterized raw SQL for the locking query if that makes the behavior clearer and more precise.
- Commit the claim before performing network I/O. Do not hold a database transaction open while waiting for a target URL.
- Define job states and legal transitions.
- Make job creation and rescheduling safe against repeated scheduler passes.

**Think about:** Multiple workers may race for the same work. A claim must be coordinated by the database, not by an in-memory mutex that exists only inside one process.

**Done when:**
- Two local worker processes can compete for jobs without normally claiming the same active job.
- Claims are durable and have an expiry/recovery policy.
- No transaction remains open during an HTTP probe.

## Step 12 - Create the separate worker executable

**Goal:** Run API and background processing as separate processes from the same repository.

**Do:**
- Add `cmd/pulse-worker`.
- Reuse the checker, configuration, database, and application logic packages where appropriate.
- Implement a dispatcher that claims due jobs and sends them to a bounded pool of check workers.
- Apply timeouts and cancellation to each probe.
- Persist results and schedule the next check according to Step 10.
- Keep the worker independent of Gin.

**Think about:** A process boundary changes failure behavior. The API can remain available while a worker restarts, but queued work must remain durable.

**Done when:**
- API and worker can run independently.
- Restarting the worker does not erase queued jobs or history.
- Concurrency limits apply across a worker process.

## Step 13 - Handle crashes, leases, retries, and duplicates

**Goal:** Make background processing recover instead of silently losing work.

**Do:**
- Recover jobs whose lease expired after a worker crash.
- Use bounded retries with backoff for retryable infrastructure failures.
- Separate retryable failures from permanent/invalid work.
- Ensure repeated job delivery does not create duplicate check records or corrupt monitor state. Choose and document an idempotency strategy.
- Make worker shutdown stop new claims, cancel or finish in-flight checks according to policy, and release or allow expiry of claims safely.
- Add tests for worker termination after claim, timeout, database interruption, and retry exhaustion.

**Think about:** Do not claim “exactly once” execution. A worker can perform an action and crash before acknowledging it. Design for at-least-once delivery and idempotent effects where practical.

**Done when:**
- Expired claims can be recovered.
- Retries are bounded.
- Duplicate delivery does not create duplicate logical effects.
- Failure behavior is documented and tested.

## Step 14 - Test multi-process coordination

**Goal:** Demonstrate that the design works across processes, not only goroutines.

**Do:**
- Run two or more worker instances against the same PostgreSQL database.
- Enqueue more jobs than one worker can handle.
- Verify jobs are claimed, processed, and recorded.
- Kill a worker while it owns work and verify recovery after lease expiry.
- Test the worker under database contention.
- Record the limits of the design: PostgreSQL is the queue for this version and can become a bottleneck at higher scale.

**Think about:** A mutex protects shared memory inside a process. Database locks and durable state coordinate independent processes. They solve different problems.

**Done when:**
- A repeatable integration test exercises multiple workers.
- Crash recovery is demonstrated.
- You can explain the job lifecycle and its failure windows without looking at the code.

---

# Phase D - Incidents, alerts, and user experience

## Step 15 - Implement incident state transitions

**Goal:** Turn individual check results into meaningful incidents.

**Do:**
- Define a simple policy, for example: open an incident after three consecutive failed checks and resolve it after a successful check.
- Store incident start, resolution, monitor ID, and relevant summary fields.
- Ensure repeated failed checks do not open a new incident every time.
- Handle recovery and repeated outages.
- Use transactions or guarded updates so concurrent workers cannot create inconsistent incident state.

**Think about:** A failed check is an observation. An incident is a stateful product concept derived from a sequence of observations.

**Done when:**
- Consecutive failures open one incident.
- Recovery resolves the active incident.
- Repeated failures do not create duplicate active incidents.

## Step 16 - Send webhook alerts reliably enough for v1

**Goal:** Notify users about incident opening and recovery without blocking monitoring.

**Do:**
- Allow a user to configure a webhook destination with clear validation and ownership.
- Send notifications from a separate asynchronous path backed by durable work, or another explicitly documented retryable mechanism.
- Use timeouts, bounded retries, and sanitized logs.
- Avoid sending secrets in logs or exposing them in API responses.
- Consider webhook SSRF risks: restrict schemes, reject local/private destinations where appropriate, and document remaining limitations.
- Ensure a failed webhook does not prevent the check result from being saved.

**Think about:** Alert delivery is an external dependency. Monitoring must keep working when the notification provider is down.

**Done when:**
- Incident-open and incident-resolved events can generate notifications.
- Notification failures are retried within a defined limit.
- A failed webhook cannot crash the worker or lose the check result.

## Step 17 - Build the dashboard

**Goal:** Make Pulse usable by someone other than its developer.

**Do:**
- Provide sign-up/login and a clear monitor list.
- Show current status, last check time, latency, and incident state.
- Add a monitor form, monitor detail page, and recent history view.
- Include loading, empty, validation-error, and API-error states.
- Make status distinguishable without relying on color alone.
- Keep visual scope focused. Do not build a general-purpose frontend framework.

**Think about:** The UI should reflect the API's actual state. Do not invent a status locally when the backend has not confirmed it.

**Done when:**
- A user can sign up, add a URL, see its latest result, and inspect history without using curl.
- Common failure states are understandable.
- The layout works on desktop and a narrow screen.

---

# Phase E - Production readiness and project finish

## Step 18 - Add configuration and operational safety

**Goal:** Make local, test, and production configuration explicit.

**Do:**
- Validate required environment variables at startup.
- Separate API and worker configuration where necessary.
- Set database pool limits and timeouts deliberately.
- Ensure URLs and secrets are not written to logs unnecessarily.
- Configure HTTP server, outbound client, database, and shutdown timeouts.
- Document migrations and startup ordering.

**Done when:**
- A missing required setting causes a clear startup error.
- No secret is needed in source code.
- Both API and worker have predictable shutdown behavior.

## Step 19 - Add structured logs, metrics, and health checks

**Goal:** Make the service diagnosable without attaching a debugger.

**Do:**
- Use `slog` with consistent structured fields.
- Log request/job IDs where useful, monitor ID, result, duration, retry count, and failure category.
- Add metrics for checks attempted/succeeded/failed, check latency, queue depth or age, job retries, and alert failures.
- Keep labels bounded: do not use raw URLs, user IDs, or other unbounded values as metric labels.
- Define readiness and liveness behavior for API and worker.

**Think about:** Logs explain individual events; metrics reveal aggregate behavior and trends.

**Done when:**
- You can identify a failed check and follow its processing path.
- Metrics can show failure rate and queue backlog.
- Metrics endpoints do not expose secrets or sensitive user data.

## Step 20 - Build a meaningful test suite

**Goal:** Verify behavior, ownership, concurrency, and recovery.

**Do:**
- Unit-test checker classification, interval rules, incident transitions, retry policy, and validation.
- Integration-test database queries and migrations against PostgreSQL.
- API-test authentication, CRUD, ownership, pagination, and error responses.
- Test concurrent job claiming and expired-lease recovery.
- Run `go test ./...`, `go test -race ./...`, and `go vet ./...`.
- Add tests for regressions whenever a real bug is found.

**Done when:**
- Core business rules have automated tests.
- Integration tests can be run from documented commands.
- The race detector and vet pass for the project.
- No test requires hitting an uncontrolled public website.

## Step 21 - Containerize and run the complete stack locally

**Goal:** Make a reproducible local environment.

**Do:**
- Create a multi-stage Dockerfile or equivalent images for the API and worker.
- Add Docker Compose services for PostgreSQL, API, and worker.
- Use health checks and explicit dependency readiness rather than assuming container startup means the database is ready.
- Keep persistent database data in a volume.
- Use a non-root runtime user where practical.
- Document migration execution and local reset procedures.

**Done when:**
- A new developer can start the complete stack from the README.
- API and worker are separate processes.
- Data survives a container restart.
- A clean checkout can be built without machine-specific files.

## Step 22 - Add CI and deploy

**Goal:** Prove that the project can be built and run outside your development machine.

**Do:**
- Add CI for formatting checks, tests, race tests where practical, vet, and build.
- Choose a hosting setup that can run a persistent worker and PostgreSQL; confirm platform limitations before choosing it.
- Configure secrets through the hosting platform.
- Apply migrations using a controlled deployment step.
- Verify health endpoints and test a real monitor after deployment.
- Document rollback and database backup limitations for the chosen hosting setup.

**Done when:**
- CI runs on pushes and pull requests.
- The deployed API is reachable.
- A deployed worker performs checks and saves history.
- You have tested a restart and verified the system recovers.

## Step 23 - Document the system like an engineer

**Goal:** Make the project understandable and credible in a code review or interview.

**Do:**
- Write a README covering the product, features, local setup, environment variables, migrations, tests, and deployment.
- Add an architecture diagram and a sequence diagram for one monitor check.
- Document job states, lease expiry, retries, idempotency, and incident rules.
- Record key trade-offs: PostgreSQL queue versus a broker, bounded concurrency, retention, and what would change at higher scale.
- Include screenshots and a short demo.
- Document known limitations honestly.

**Done when:**
- Someone can run the project without asking you for missing steps.
- You can explain the complete lifecycle from monitor creation to check result to alert.
- The README describes implemented behavior, not future aspirations.

## Step 24 - Final acceptance test and freeze

**Goal:** Finish the defined product rather than expanding it indefinitely.

Run the following end-to-end scenario:

1. Start the stack from a clean environment.
2. Register two users.
3. Create monitors for both users.
4. Confirm ownership boundaries by attempting cross-user access.
5. Run multiple worker processes and confirm jobs are coordinated.
6. Make a test target return success, then fail repeatedly, then recover.
7. Confirm check history, incident opening/resolution, and webhook notifications.
8. Restart a worker during claimed work and confirm recovery.
9. Run the automated test suite and CI.
10. Verify the deployed dashboard and API, and review logs and metrics.
11. Complete the README and demo.

**Done when:**
- All required product behavior works.
- Security, integration, and recovery tests pass.
- The application is deployed or the documented deployment limitation is explicit.
- The repository is clean and the demo is reproducible.

**After Step 24: stop.** Do not add features to this version. Write a short retrospective listing what you learned, what remains limited, and the next project or focused experiment that would teach you the next set of skills.

---

## 4. Progress tracker

Mark each step only when its acceptance criteria pass.

- [ ] 01 - Reproducible baseline
- [ ] 02 - Correct, testable checker
- [ ] 03 - Bounded concurrency
- [ ] 04 - PostgreSQL and persistence boundaries
- [ ] 05 - Gin API skeleton
- [ ] 06 - API contract
- [ ] 07 - Authentication
- [ ] 08 - Monitor CRUD and ownership
- [ ] 09 - Check history
- [ ] 10 - Scheduling rules
- [ ] 11 - Durable PostgreSQL job queue
- [ ] 12 - Separate worker executable
- [ ] 13 - Crash recovery, retries, idempotency
- [ ] 14 - Multi-process coordination tests
- [ ] 15 - Incident state transitions
- [ ] 16 - Webhook alerts
- [ ] 17 - Dashboard
- [ ] 18 - Configuration and operational safety
- [ ] 19 - Logs, metrics, health
- [ ] 20 - Test suite
- [ ] 21 - Docker Compose
- [ ] 22 - CI and deployment
- [ ] 23 - Engineering documentation
- [ ] 24 - Final acceptance and freeze

## 5. Rules against endless perfection

1. **Acceptance criteria beat vague quality goals.** “Production-grade” does not mean every conceivable feature. It means the defined behavior is secure enough for the intended scope, tested, observable, documented, and deployable.
2. **One fix, one reason.** Refactor when it removes duplication, clarifies ownership, fixes a demonstrated issue, or enables a required test-not merely because another pattern exists.
3. **No unplanned infrastructure.** A new service or dependency needs a written problem statement and a reason the current design cannot meet the requirement.
4. **No premature scale claims.** Test realistic local loads and describe measured results. Do not claim internet-scale reliability based on a portfolio deployment.
5. **Finish the vertical slice.** A small complete feature is more valuable than several half-built abstractions.
6. **Record learning separately from implementation.** Each step should leave you with a short explanation of the concept and its trade-offs.
7. **Freeze after Step 24.** Bugs and security defects can still be fixed, but new product ideas go into a future-version list.

## 6. The reasoning checklist

Before implementing any component, answer:

- What input does it receive?
- What output or state change does it produce?
- Who owns this responsibility?
- What state must survive a restart?
- What happens if it times out or crashes?
- Can the operation run twice?
- Can multiple goroutines or processes execute it concurrently?
- What limits resource usage?
- How will we test the failure path?
- What evidence tells us the step is complete?

**Start at Step 1.** Do not start the Gin rewrite or distributed-worker implementation until the current baseline and checker behavior are understood. GORM is the planned ORM for ordinary persistence; Goose remains responsible for versioned schema migrations.
