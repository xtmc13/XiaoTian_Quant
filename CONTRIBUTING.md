# Contributing to XiaoTianQuant

Thank you for your interest in contributing! This document covers everything you need to know.

## Table of Contents

- [Code of Conduct](#code-of-conduct)
- [Getting Started](#getting-started)
- [Development Setup](#development-setup)
- [Project Structure](#project-structure)
- [Coding Standards](#coding-standards)
- [Submitting Changes](#submitting-changes)
- [Testing](#testing)
- [Documentation](#documentation)

## Code of Conduct

We are committed to providing a welcoming and inclusive environment. All contributors are expected to:

- Be respectful and constructive in discussions
- Accept constructive criticism gracefully
- Focus on what is best for the community
- Show empathy towards other community members

## Getting Started

### Prerequisites

| Tool | Minimum Version | Recommended |
|------|----------------|-------------|
| Go | 1.23 | 1.25 |
| Rust | 1.75 | stable |
| Node.js | 18 | 22 |
| Python | 3.12 | 3.12 |
| Docker | 24+ | latest |

### One-Click Install

```bash
# Linux/macOS
chmod +x install.sh && ./install.sh

# Windows (PowerShell as Administrator)
.\install.ps1
```

### Manual Setup

```bash
# 1. Clone the repo
git clone https://github.com/your-org/xiaotian-quant.git
cd xiaotian-quant

# 2. Environment variables
cp .env.example .env
# Edit .env with your values

# 3. Build Rust matching engine
cd engine && cargo build --release && cd ..

# 4. Build Go gateway
cd gateway && go mod download && go run ./cmd/server/
```

## Project Structure

```
xiaotian_quant/
├── gateway/           # Go backend (core business logic)
├── engine/            # Rust matching engine (high-performance)
├── web/               # React 19 frontend (SPA)
├── sandbox/           # Python ML/indicator sandbox
├── docs/              # Documentation (MkDocs)
├── scripts/           # Deployment scripts
├── nginx/             # Nginx config + SSL
├── .github/workflows/ # CI/CD
├── Dockerfile         # Multi-stage Docker build
├── docker-compose.yml # Development environment
└── Makefile           # Common commands
```

See each module's `README.md` for detailed documentation.

## Coding Standards

### Go (`gateway/`)

- Use `gofmt` (required) and `goimports`
- Every exported function/type must have a doc comment (`// MyFunc does X...`)
- Handle all errors explicitly — no bare `_` on error returns
- Use `context.Context` for cancellation and timeouts
- Keep functions under 100 lines when possible
- Tests: every business-critical module must have tests

```bash
# Lint
make lint

# Format
gofmt -w .
goimports -w .

# Test
go test ./internal/... -race -count=1
```

### Rust (`engine/`)

- Follow `rustfmt` formatting
- Use `clippy` for lints: `cargo clippy -- -D warnings`
- Document public APIs with `///` doc comments
- Add tests for matching logic and FFI

```bash
# Check
cargo check && cargo clippy -- -D warnings

# Test
cargo test

# Benchmark
cargo bench
```

### TypeScript/React (`web/`)

- Strict TypeScript mode enabled
- ESLint flat config with React hooks and accessibility plugins
- Prettier formatting (single quote, 2 spaces, trailing comma)
- Component files: one component per file
- Custom hooks prefixed with `use`

```bash
cd web

# Lint
npm run lint

# Format
npm run format

# Type check
npm run type-check

# Test
npm run test
```

### Python (`sandbox/`)

- Type hints required for all function signatures
- Docstrings (Google style) for all modules and public functions
- Black formatting (if installed)

## Submitting Changes

### Git Workflow

1. **Fork** the repository
2. **Create a feature branch**: `git checkout -b feature/my-feature`
3. **Make changes** with clear commit messages
4. **Run tests** for the affected module
5. **Push** and **open a Pull Request**

### Commit Message Convention

We follow [Conventional Commits](https://www.conventionalcommits.org/):

```
<type>(<scope>): <description>

[optional body]

[optional footer]
```

**Types**: `feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `test`, `chore`

**Examples**:
```
feat(engine): add GTC order type support
fix(gateway): resolve config env var expansion
docs(web): add component documentation
test(rust): add matching engine unit tests
```

### Pull Request Checklist

- [ ] Tests pass (`make test`)
- [ ] Code follows style guidelines
- [ ] Self-reviewed the code
- [ ] Updated documentation as needed
- [ ] Added tests for new functionality
- [ ] No new warnings from linters

## Testing

| Layer | Command | Location |
|-------|---------|----------|
| Go unit tests | `go test ./internal/... -race` | `gateway/internal/` |
| Rust tests | `cargo test` | `engine/src/` |
| Frontend unit | `npm run test` | `web/src/` |
| E2E | `npm run test:e2e` | `web/e2e/` |

## Documentation

- **Architecture**: See `ARCHITECTURE.md` in the root
- **API**: `gateway/docs/openapi.yaml` + `docs/api/rest-api.md`
- **Strategy guide**: `docs/strategy-guide.md`
- **Deployment**: `DEPLOYMENT.md`

## Need Help?

- Open an [Issue](https://github.com/your-org/xiaotian-quant/issues) for bugs or feature requests
- Check existing issues and discussions before creating new ones
