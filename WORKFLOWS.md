# Renovate-Trigger — Workflow Diagrams

`renovate-trigger` reacts to GitHub tag events and runs Renovate on the repos
that **consume** the tagged code. A tag on a **dependency** repo is batched over a
short window; at flush it reads each dependency's `renovate.trigger.json` (on the
default branch) to resolve its **dependents**, then — if no Renovate run is
already active — creates a one-off `Job` cloned from an existing Renovate
`CronJob`, injecting the dependents via `RENOVATE_REPOSITORIES`.

A single **GitHub App** both delivers the webhooks and grants read access to the
trigger files. See `CONTEXT.md` for vocabulary and `docs/adr/` for the decisions.

> Diagrams reflect the intended design: decentralized opt-in via
> `renovate.trigger.json`, GitHub-App auth (client-ID JWT, per-repo installation
> discovery), flush-time resolution, and best-effort mutual exclusion.

---

## 1. Component Architecture

How the packages wire together, from `main.go` down to the external APIs.

```mermaid
flowchart TD
    subgraph ext[External]
        GH[GitHub App<br/>webhooks + contents API]
        KUBE[Kubernetes API]
    end

    subgraph app[renovate-trigger process]
        MAIN["main.go<br/>(wiring + lifecycle)"]
        CFG["config.Load<br/>(RT_* env vars)"]
        SRV["server.Server<br/>HTTP mux"]
        HDL["webhook.Handler"]
        SIG["signature.ValidateSignature<br/>(HMAC-SHA256)"]
        COL["batch.Collector<br/>(deduped set + timer)"]
        GATE["RunGate<br/>(mutual exclusion)"]
        RES["Resolver<br/>(sources → dependents)"]
        GHC["github client<br/>(JWT + installation)"]
        JOB["k8s.JobCreator"]
        CLI["k8s in-cluster client"]
    end

    CFG --> MAIN
    MAIN --> SRV
    MAIN --> HDL
    MAIN --> COL
    MAIN --> GATE
    MAIN --> RES
    MAIN --> JOB

    GH -- "POST /webhook" --> SRV
    SRV --> HDL
    HDL --> SIG
    HDL -- "Add(sourceRepo)" --> COL
    COL -- "attemptFlush" --> GATE
    GATE --> CLI
    COL -- "when clear" --> RES
    RES --> GHC
    GHC <--> GH
    COL -- "onFlush(dependents)" --> JOB
    JOB --> CLI
    CLI <--> KUBE
    SRV -- "/readyz probes" --> CLI
```

---

## 2. Startup & Graceful Shutdown

Lifecycle managed in `cmd/renovate-trigger/main.go`. Startup fails loud on any
config error; it does **not** mint a GitHub token at boot.

```mermaid
flowchart TD
    START([Process start]) --> LOAD[config.Load<br/>required env, log level]
    LOAD -->|error| EXIT1[log error<br/>os.Exit 1]
    LOAD -->|ok| KEY[Read + parse<br/>App private key PEM]
    KEY -->|error| EXIT2[log error<br/>os.Exit 1]
    KEY -->|ok| K8S[k8s.NewInClusterClient]
    K8S -->|error| EXIT3[os.Exit 1]
    K8S -->|ok| CJ["get source CronJob<br/>(assert it exists)"]
    CJ -->|error| EXIT4[log error<br/>os.Exit 1]
    CJ -->|ok| WIRE["Build github client, Resolver,<br/>RunGate, JobCreator,<br/>Collector, Handler, Server"]
    WIRE --> SIGCTX[signal.NotifyContext<br/>SIGINT / SIGTERM]
    SIGCTX --> GO[go srv.Start]
    GO --> READY[Log 'started',<br/>serving requests]
    READY --> WAIT{ctx.Done?}
    WAIT -->|signal received| SHUT[srv.Shutdown<br/>10s timeout]
    SHUT --> STOPCOL["collector.Stop<br/>(cancel timer — pending<br/>batch is dropped)"]
    STOPCOL --> DONE([Log 'stopped', exit])

    GO -.->|server error| EXIT5[os.Exit 1]
```

---

## 3. Webhook Request Handling

`internal/webhook/handler.go` — validation and event-filtering gauntlet. No
GitHub calls happen here; an accepted event just adds the **source** repo to the
batch. Opt-out (no trigger file) is discovered later, at flush.

```mermaid
flowchart TD
    REQ([POST /webhook]) --> M{Method == POST?}
    M -->|no| E405[405 method not allowed]
    M -->|yes| READ[Read body<br/>MaxBytesReader 1MB]
    READ -->|read error| E400a[400 bad request]
    READ -->|ok| SIG{Valid HMAC-SHA256<br/>X-Hub-Signature-256?}
    SIG -->|no| E403["403 forbidden<br/>(warn log)"]
    SIG -->|yes| EVT{X-GitHub-Event<br/>== 'create'?}
    EVT -->|no| I1["200 ignored<br/>'not a create event'"]
    EVT -->|yes| PARSE[json.Unmarshal<br/>CreateEvent]
    PARSE -->|error| E400b[400 bad request]
    PARSE -->|ok| RT{ref_type == 'tag'?}
    RT -->|no| I2["200 ignored<br/>'ref_type is not tag'"]
    RT -->|yes| ADD["batch.Add(source repo)"]
    ADD --> ACC["202 accepted<br/>{repo, tag}"]

    classDef reject fill:#fde,stroke:#c33;
    classDef ignore fill:#ffd,stroke:#cc3;
    classDef ok fill:#dfd,stroke:#3c3;
    class E405,E400a,E403,E400b reject;
    class I1,I2 ignore;
    class ACC ok;
```

> The opt-in gate (App installed + trigger file present) is not enforced here —
> a source repo with no `renovate.trigger.json` is accepted, then contributes
> nothing at resolution time (logged DEBUG).

---

## 4. Batch Collection, Gate & Flush

`internal/batch/collector.go` — a fixed tumbling window over **source** repos.
On fire, the flush is gated by mutual exclusion; if a Renovate run is active it
postpones and keeps accumulating.

```mermaid
flowchart TD
    A["Add(sourceRepo)"] --> LOCK[Lock mutex]
    LOCK --> PUT[sources set += repo<br/>deduped]
    PUT --> T{timer == nil?}
    T -->|yes| ARM["time.AfterFunc(window, attemptFlush)"]
    T -->|no| SKIP[reuse running timer]
    ARM --> UNLOCK[Unlock]
    SKIP --> UNLOCK

    ARM -. "window elapses" .-> AF["attemptFlush()"]
    AF --> EMPTY{sources empty?}
    EMPTY -->|yes| RESET[timer = nil, no-op]
    EMPTY -->|no| GATE{"RunGate.Active()?<br/>(list Jobs)"}
    GATE -->|"active"| POSTPONE["re-arm timer (window)<br/>keep sources<br/>WARN if postponed too long"]
    GATE -->|"clear"| DRAIN[Copy sources, clear set,<br/>timer = nil]
    DRAIN --> RESOLVE["Resolver.Resolve(sources)<br/>→ deduped dependents"]
    RESOLVE --> UNION{union empty?}
    UNION -->|yes| NOOP["no Job<br/>(INFO)"]
    UNION -->|no| CB["onFlush(dependents)<br/>→ JobCreator"]
```

---

## 5. Mutual Exclusion (RunGate)

Whether a Renovate run is already active. Best-effort — the CronJob is not
suspended, so a small TOCTOU window against the CronJob controller is accepted.

```mermaid
flowchart TD
    G["RunGate.Active(ctx)"] --> LIST["Jobs(cronjobNs).List()"]
    LIST -->|error| ERR["return error<br/>(treat as active → postpone)"]
    LIST -->|ok| SCAN[For each Job]
    SCAN --> DONE{Complete or Failed?}
    DONE -->|yes| NEXT[ignore]
    DONE -->|no| WHO{ours OR owned<br/>by source CronJob?}
    WHO -->|"managed-by=renovate-trigger"| ACTIVE[return true]
    WHO -->|"ownerRef → CronJob name"| ACTIVE
    WHO -->|neither| NEXT
    NEXT --> ANY{more jobs?}
    ANY -->|yes| SCAN
    ANY -->|no| CLEAR[return false]
```

---

## 6. Resolution (Resolver + GitHub client)

`Resolver` expands the batch of source repos into the deduped union of their
dependents by reading each `renovate.trigger.json`. Per-source failures degrade
independently (one broken file never sinks the batch).

```mermaid
flowchart TD
    R["Resolve(ctx, sources)"] --> LOOP[For each unique source]
    LOOP --> INST["github: discover installation<br/>for owner/repo (app JWT)"]
    INST -->|error| WARN1["WARN, drop source"]
    INST -->|ok| TOK["mint/cache installation token"]
    TOK --> FETCH["GET contents<br/>renovate.trigger.json @ default branch"]
    FETCH -->|404| DBG["DEBUG opt-out, skip"]
    FETCH -->|error| WARN2["WARN, drop source"]
    FETCH -->|ok| PARSE{parse JSON<br/>tags[]?}
    PARSE -->|malformed| WARN3["WARN, drop source"]
    PARSE -->|ok| ENTRIES["for each dependent:<br/>valid owner/repo? → add<br/>else WARN skip"]
    ENTRIES --> ACC[accumulate into union set]
    ACC --> MORE{more sources?}
    MORE -->|yes| LOOP
    MORE -->|no| RET["return deduped dependents"]
```

---

## 7. Kubernetes Job Creation

`internal/k8s/job.go` — clone the CronJob's `JobTemplate`, inject the resolved
dependents, create.

```mermaid
flowchart TD
    F["CreateJobForRepos(ctx, dependents)"] --> GET["CronJobs(ns).Get(name)"]
    GET -->|error| ERR1[return error<br/>'getting cronjob']
    GET -->|ok| COPY["JobTemplate.Spec.DeepCopy<br/>(never mutate cache)"]
    COPY --> MARSHAL["json.Marshal(dependents)<br/>→ '[\"org/a\",\"org/b\"]'"]
    MARSHAL -->|error| ERR2[return error]
    MARSHAL -->|ok| OVERRIDE["For each container:<br/>overrideEnv RENOVATE_REPOSITORIES<br/>(replace or append)"]
    OVERRIDE --> BUILD["Build Job:<br/>GenerateName 'renovate-trigger-'<br/>label managed-by<br/>annotations dependents + triggered-at"]
    BUILD --> CREATE["Jobs(ns).Create(job)"]
    CREATE -->|error| ERR3[return error<br/>'creating job']
    CREATE -->|ok| RET[log + return job name]
```

---

## 8. Health & Readiness Probes

`internal/server/server.go` — liveness always OK; readiness checks only the K8s
API (5s cache). Readiness does **not** depend on GitHub or on the CronJob.

```mermaid
flowchart TD
    subgraph Liveness
        LZ([GET /healthz]) --> L200[200 ok<br/>always]
    end

    subgraph Readiness
        RZ([GET /readyz]) --> CACHE{cache age < 5s?}
        CACHE -->|yes| USE{cached ready?}
        USE -->|true| R200a[200 ok]
        USE -->|false| R503a[503 not ready]
        CACHE -->|no| PING["k8s Discovery().ServerVersion()"]
        PING -->|success| SET1[cache=true, stamp time]
        PING -->|error| SET0[cache=false, stamp time<br/>warn log]
        SET1 --> R200b[200 ok]
        SET0 --> R503b[503 not ready]
    end
```

---

## 9. Deployment Topology (Helm)

`chart/` — single replica, least-privilege RBAC scoped to the CronJob namespace.
GitHub App credentials come from a single existing Secret (not created by the
chart; e.g. provisioned by External Secrets Operator).

```mermaid
flowchart LR
    subgraph cluster[Kubernetes Cluster]
        subgraph nsapp[renovate-trigger namespace]
            DEP["Deployment<br/>replicas:1, Recreate<br/>(nonRoot, RO rootfs,<br/>drop ALL caps)"]
            SVC[Service :8080]
            SA[ServiceAccount]
            SEC["Existing Secret (not chart-created)<br/>client ID + private key + webhook secret"]
            RB[RoleBinding]
        end
        subgraph nscron[CronJob namespace]
            ROLE["Role<br/>cronjobs: get<br/>jobs: create, list"]
            CRON[Renovate CronJob]
            NEWJOB[Created Jobs]
        end
    end
    GHAPP[GitHub App] --> SVC --> DEP
    SEC --> DEP
    DEP -- "read contents" --> GHAPP
    SA --> DEP
    SA --> RB --> ROLE
    DEP -- "get" --> CRON
    DEP -- "create / list" --> NEWJOB
```
