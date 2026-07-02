# Chaos — Separação SLO vs Timeout Operacional

## Status: Implementado (convenção a manter)

## Contexto

Durante a implementação dos cenários de chaos testing, surgiu a tentação de aumentar valores de SLO para acomodar limitações de infraestrutura (ex: Docker healthcheck timing). Isso foi explicitamente rejeitado como abordagem.

## Regra

SLO e timeout operacional são conceitos diferentes e não devem ser acoplados:

- **SLO (Service Level Objective)**: Representa o objetivo de recuperação que estamos medindo. Se o sistema não atinge o SLO, o cenário deve falhar — isso é informação válida.
- **Timeout operacional**: Representa o tempo máximo que o código espera antes de desistir (ex: `WaitForHealthy`). Deve ser >= SLO para permitir a medição, mas não define o critério de sucesso.

### Exemplo concreto

```
aiFailureRecoverySLO = 60s    // SLO: queremos recovery em < 60s
WaitForHealthy timeout = 120s  // Operacional: esperamos até 120s para medir
```

Se o recovery levar 90s:
- O `WaitForHealthy` não faz timeout (90s < 120s) ✓
- O SLO falha (90s > 60s) ✓ — isso é o comportamento correto

Se tivéssemos acoplado (SLO = timeout = 120s):
- O SLO passaria (90s < 120s) ✗ — esconde uma regressão real

## Aplicação

Em todos os cenários de chaos, ao adicionar ou modificar SLOs:

1. Definir o SLO como constante separada baseada no objetivo de recuperação
2. Usar timeouts operacionais independentes nos `WaitFor*` e `WaitForHealthy`
3. Nunca ajustar o SLO para "fazer o teste passar"

## Arquivos relacionados

- `cmd/chaos/internal/chaos/scenarios/ai_failure.go` — `aiFailureRecoverySLO` vs `WaitForHealthy` timeout
- `cmd/chaos/internal/chaos/scenarios/runtime_hang.go` — cleanup `WaitForHealthy` separado do SLO de detecção
