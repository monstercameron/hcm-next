# Simulated Compensation Market API

This package is a separate simulated third-party datasource. It is intentionally
outside `src/api`, which is the internal Human Capital Management Suite API server.

Run it locally:

```bash
npm run dev:third-party:compensation
```

Default base URL:

```text
http://localhost:4301
```

Example lookup:

```bash
curl -sS "http://localhost:4301/v1/market-pricing?jobCode=ENG-SWE3&level=P3&payZone=US-WEST"
```
