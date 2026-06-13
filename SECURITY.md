# Security Policy

## Supported Versions

| Version | Supported |
|---------|-----------|
| 3.x (current) | ✅ |
| 2.x | ⚠️ Legacy |
| 1.x | ❌ |

## Reporting a Vulnerability

We take security seriously. If you discover a security vulnerability, please report it responsibly.

### How to Report

1. **Do NOT** open a public GitHub issue for security vulnerabilities
2. Email: **security@xtquant.com** (or open a [private security advisory](https://github.com/your-org/xiaotian-quant/security/advisories/new))
3. Include:
   - Description of the vulnerability
   - Steps to reproduce
   - Potential impact
   - Suggested fix (if any)

### What to Expect

- **Acknowledgment**: Within 48 hours
- **Assessment**: Within 1 week
- **Fix timeline**: Critical fixes within 7 days, others within 30 days
- **Disclosure**: We will coordinate with you on public disclosure timing

## Security Best Practices

### For Users

1. **Never commit secrets** — use `.env` files (already in `.gitignore`)
2. **Rotate keys** if accidentally committed — check with `git log --all --full-history -- <file>`
3. **Use strong passwords** — enforce minimum complexity
4. **Keep dependencies updated** — run `npm audit`, `go list -m all -u`, `cargo audit`
5. **Run in production with HTTPS** — use Let's Encrypt or your CA
6. **Restrict pprof access** — only enable in debug mode, restrict to localhost

### For Contributors

1. **Pre-commit hooks**: Always run linters before committing
2. **No hardcoded secrets**: Use environment variables or config templates
3. **Code review**: All PRs require at least one review
4. **CI security scan**: Automated dependency scanning runs on every PR

## Known Security Controls

| Control | Status | Details |
|---------|--------|---------|
| JWT Authentication | ✅ | HS256, 24h expiry, refresh tokens |
| Rate Limiting | ✅ | Token bucket per-IP, stricter on auth endpoints |
| CORS | ✅ | Configurable allowed origins |
| SQL Injection | ✅ | SQLite prepared statements via modernc.org/sqlite |
| XSS | ✅ | React auto-escaping, CSP headers |
| SSL/TLS | ✅ | Nginx reverse proxy with TLS termination |
| pprof Access | ✅ | Restricted to localhost in production |
| Container Security | ✅ | Non-root user, minimal Alpine base image |

## Dependency Security

```bash
# Go dependencies
cd gateway && go list -m all -u

# Node.js dependencies
cd web && npm audit

# Rust dependencies
cd engine && cargo audit  # requires cargo-audit crate

# Docker image scanning
docker scan <image-name>
```

## Incident Response

If a vulnerability is discovered:

1. **Contain**: Disable affected endpoints/services
2. **Assess**: Determine scope and impact
3. **Fix**: Develop and test the patch
4. **Deploy**: Hotfix to production (critical only)
5. **Notify**: Affected users if personal data was compromised
6. **Document**: Update CHANGELOG and security advisories
