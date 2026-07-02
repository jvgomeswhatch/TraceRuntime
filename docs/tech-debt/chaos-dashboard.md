# Chaos Dashboard

## Status: Implementado (Phase 11) — com débitos técnicos pendentes

## Prioridade: Baixa

## O que foi entregue

- Backend: 4 endpoints (ListReports, GetReport, Trigger, Status)
- Frontend: página lista com trigger form + página detalhe com SLOs e stages
- Sidebar: link "Chaos" adicionado
- Testes de integração: 10 cenários
- Segurança: validação de report ID, path traversal bloqueado, whitelist de cenários

## Débitos técnicos

### 1. Timeout do trigger não é utilizado pelo runner

O endpoint `/api/chaos/trigger` aceita `timeout_seconds` e salva no arquivo de request, mas o chaos runner (`cmd/chaos/`) não lê esse arquivo — ele usa seu próprio timeout padrão (900s) ou flag `--timeout`. O campo foi removido do frontend, mas o backend ainda aceita e persiste o valor sem efeito.

**Solução futura:** Implementar um chaos-runner watcher que leia os arquivos de `results/chaos-requests/` e execute o cenário com os parâmetros especificados (timeout, scenario), eliminando a necessidade de `make chaos` manual.

### 2. Cenários de chaos não são configuráveis pelo operador

Os 6 cenários (worker-crash, runtime-hang, ai-failure, postgres-failure, queue-flood, slow-inference) são hardcoded no runner Go. O operador só pode escolher qual rodar, mas não pode customizar parâmetros como: duração da injeção, quantidade de mensagens no flood, SLO thresholds, etc.

**Solução futura:** Arquivo de configuração YAML/JSON onde o operador define cenários customizados com parâmetros ajustáveis, similar ao que Litmus e Chaos Mesh oferecem.

### 3. Execução requer intervenção manual

Após trigger no dashboard, o operador precisa rodar `make chaos` no terminal manualmente. Não há execução automática.

**Solução futura:** Container watcher que monitora `results/chaos-requests/` e executa automaticamente, ou integração com CI/CD para execução programada.

## Esforço estimado

- Watcher automático: 2-3 dias
- Cenários configuráveis: 3-5 dias
- Ambos são melhorias opcionais — o dashboard atual já cumpre seu propósito de visualização e trigger operacional
