# HarborCare demo company

HarborCare Health Services is a wholly fictional company used to exercise the
production Go/WASM experience. It is a multi-state community-care provider
that combines clinical operations, care coordination, and a digital member
platform. Its legal entity is **HarborCare Health Services, Inc.** and its
fictional headquarters is Boston, Massachusetts.

The demo workforce is intentionally separate from the frozen four-worker
promotion conformance corpus. Product demonstrations may grow without changing
the regression vectors that prove workflow behavior.

## Organization

```text
HarborCare Health Services
├── Executive Office
├── Care Operations
│   ├── Clinical Operations
│   ├── Care Coordination
│   └── Quality & Safety
├── Product & Technology
│   ├── Engineering Platform
│   ├── Product Management
│   ├── Data & Analytics
│   └── Security & IT
├── Growth & Customer
│   ├── Customer Success
│   ├── Sales
│   └── Marketing
└── Corporate Services
    ├── People Operations
    ├── Finance
    ├── Legal & Compliance
    └── Workplace Services
```

The deterministic seed records the legal entity and all 20 hierarchy nodes in
the bitemporal organization aggregate, then contributes 60 active employees across these units,
with executive, clinical, technical, product, analytics, security, customer,
sales, marketing, people, finance, legal, compliance, and workplace roles.
Employees span eight US locations and six pay grades. Manager relationships
form a coherent reporting graph whose root is the fictional HarborCare board.

## Profile-photo policy

Exactly 45 of the 60 seeded employees (75 percent) have generated fictional
profile photos. The other 15 deliberately exercise the shared initials
fallback; absence is represented by absent references, never a broken URL.

Every selected image passes through `internal/humanwork/profilephoto` before a
worker row is written. The processor validates declared and sniffed MIME types,
enforces byte and pixel limits, retains the byte-exact PNG below the private,
non-embedded `demo-assets/profile-originals/` directory, center-crops a 160×160 JPEG proxy, and refuses
content-changing overwrite. PostgreSQL retains both references as an
all-or-nothing pair, while the Journey worker API exposes only the proxy. The
avatar component uses native lazy loading and asynchronous decoding.

## Sources of truth and loading

- Company, organization, people, roles, reporting lines, and photo selection:
  `internal/data/demoworkforce/plan.go`
- Upload and derivative processor: `internal/humanwork/profilephoto`
- Durable fields and invariant: `migrations/00070_worker_profile_photo_refs.sql`
- Operator entry point: `go run ./cmd/migrate demo-people -tenant harborcare-demo -photo-source demo-assets/profile-originals -original-dir demo-assets/profile-originals`

The command is deterministic and replay-safe. It processes all selected photo
sources through the upload pipeline and records people in one tenant-scoped
transaction. A replay verifies the identity-defining fields of every existing
row and reports inserted versus skipped counts.
