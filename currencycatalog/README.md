# Currency catalog

This package embeds a dated snapshot of monetary ISO 4217 currencies for offline use. It does not fetch data at runtime.

The snapshot at `snapshots/2026-09-17.json` was generated from SIX List One XML published on 2026-09-17. SIX identifies itself as the ISO 4217 Maintenance Agency and lists List One as the current currency and funds list: <https://www.six-group.com/en/products-services/financial-information/market-reference-data/data-standards.html>. The source XML is <https://www.six-group.com/dam/download/financial-information/data-center/iso-currrency/lists/list-one.xml>.

The generator uses SIX's `IsFund="true"` marker to exclude fund codes and retains rows with numeric minor units, excluding entries reserved for metals, special-purpose, test, and no-currency designations. Repeated currency codes across countries are deduplicated; conflicting names cause generation to fail.

## Updating the snapshot

Download List One XML from the official SIX page above, then run:

```sh
python3 currencycatalog/tools/update_snapshot.py /path/to/list-one.xml
```

The script writes a JSON snapshot named for the XML's `Pblshd` date. Review the generated diff, update the `go:embed` path and `ReadFile` path in `currencycatalog.go` to that dated filename, and update snapshot-specific count and behavior assertions in `currencycatalog_test.go`. Run focused package tests:

```sh
go test ./currencycatalog
```
