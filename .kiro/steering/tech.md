# Tech Stack & Build

## Backend (Go)

- Language: Go 1.25+
- Module name: `nofx`
- HTTP framework: `github.com/gin-gonic/gin`
- Binance API: `github.com/adshao/go-binance/v2`
- Hyperliquid API: `github.com/sonirico/go-hyperliquid`
- Ethereum crypto (Aster signing): `github.com/ethereum/go-ethereum`
- Property-based testing: `github.com/leanovate/gopter`
- AI communication: OpenAI-compatible chat completions API (DeepSeek, Qwen, custom)
- Config format: JSON (`config.json`)
- Data persistence: JSON files in `./data/` directory

## Frontend (React + TypeScript)

- Located in `web/` directory
- React 18 + TypeScript 5
- Build tool: Vite 6
- Styling: Tailwind CSS 3
- Charts: Recharts
- Data fetching: SWR
- State management: Zustand
- Dev server proxies `/api` to backend on port 8080

## Common Commands

```bash
# Backend
go build -o nofx          # Build binary
go run main.go             # Run directly
go test ./decision/...     # Run decision package tests
go test ./...              # Run all tests
go mod download            # Install dependencies

# Frontend
cd web && npm install      # Install frontend deps
cd web && npm run dev      # Dev server on :3000
cd web && npm run build    # Production build (tsc + vite)

# Docker
docker compose up -d --build   # Full stack deployment
./start.sh start --build       # Convenience script
```

## Testing

- Tests use `gopter` for property-based testing (PBT)
- Test files follow `*_test.go` convention in the same package
- PBT tests generate random inputs to verify correctness properties
- Run with `go test -v ./decision/...` for verbose output

## Configuration

- Runtime config: `config.json` (copy from `config.json.example`)
- API keys, exchange credentials, trader definitions, leverage settings
- Environment variables: `.env` (copy from `.env.example`)
- Backend default port: 8080, Frontend default port: 3000
