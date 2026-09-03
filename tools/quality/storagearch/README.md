# storagearch

`storagearch` is the focused ARCH-GO-025 source checker. It enforces the
direction of the storage seam without requiring a package move:

- semantic owners may not import concrete PostgreSQL/object/cache adapters;
- adapter packages may not publish business commands or invariants;
- exported adapter APIs may not leak driver/client types; and
- exported interfaces remain beside their semantic consumers.

`Check(repoRoot)` returns stable, line-addressed findings and has no network,
database, generator, or repository mutation side effects.
