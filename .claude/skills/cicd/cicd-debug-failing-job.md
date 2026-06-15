---
name: cicd-debug-failing-job
description: Diagnóstico de job quebrado no GitHub Actions. Use quando um job falhar no CI e a causa não estiver clara nos logs. Cobre erros comuns de LocalStack, timeout de health check, dependências Go, e falhas de Terraform.
---

# Skill: cicd-debug-failing-job

## Protocolo de diagnóstico

**Passo 1 — Ler o log completo do job antes de qualquer hipótese.**

Nunca assumir a causa sem ver o erro exato. Buscar no log:
- Linha com `Error:`, `FAIL`, `exit code`, `timeout`
- Última linha antes da falha

**Passo 2 — Classificar o erro por categoria:**

### LocalStack não subiu

Sintoma: `connection refused` em `localhost:4566` ou health check falhou.

Causa mais comum: health check muito rígido (`--health-retries` insuficiente).

Fix:
```yaml
options: >-
  --health-cmd "curl -sf http://localhost:4566/_localstack/health"
  --health-interval 5s
  --health-timeout 3s
  --health-retries 20   ← aumentar
  --health-start-period 10s
```

### Fila/bucket não existe no teste

Sintoma: `NoSuchQueue`, `NoSuchBucket` no teste de integração.

Causa: bootstrap não rodou antes do teste, ou rodou antes do LocalStack estar saudável.

Fix: adicionar `sleep 5` após `--health-retries` ou verificar order dos steps.

### go test falhou com `connection refused` para API

Sintoma: teste tenta `http://localhost:8082/tasks` e recebe connection refused.

Causa: step de `start api` não esperou o processo subir.

Fix: substituir `sleep 3` por polling:
```bash
timeout 30 bash -c 'until curl -sf http://localhost:8082/health; do sleep 1; done'
```

### Terraform `plan` falhou com provider error

Sintoma: `Error: Failed to query available provider packages`.

Causa: `terraform init` não rodou ou não tem acesso à internet para baixar provider.

Fix: adicionar cache de providers:
```yaml
- uses: actions/cache@v4
  with:
    path: ~/.terraform.d/plugin-cache
    key: terraform-${{ hashFiles('infra/terraform/.terraform.lock.hcl') }}
```

### go build falhou com `missing go.sum`

Sintoma: `verifying module: checksum mismatch`.

Causa: `go.sum` não commitado ou desatualizado.

Fix local: `go mod tidy && git add go.sum`.

### Timeout no teste de integração

Sintoma: `panic: test timed out after 90s`.

Causa mais comum: worker não processou (AI runtime habilitado acidentalmente, ou bug no mock path).

Verificar: `AI_RUNTIME_ENABLED=false` está setado no step do worker.

## Checklist rápido para qualquer falha

```
[ ] Log lido até o final?
[ ] LocalStack health check passou antes dos testes?
[ ] Bootstrap criou fila e bucket?
[ ] API e worker subiram antes dos testes (health check)?
[ ] AI_RUNTIME_ENABLED=false no CI?
[ ] OTEL_EXPORTER_OTLP_ENDPOINT="" no CI (sem collector)?
[ ] go.sum commitado e atualizado?
```
