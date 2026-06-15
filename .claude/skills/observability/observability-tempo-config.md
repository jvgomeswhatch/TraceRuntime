---
name: observability:tempo-config
description: Configuração do Grafana Tempo para este projeto — retenção bounded, integração com OTEL Collector via gRPC, data source no Grafana para busca de traces por trace_id, limites de memória para 14GB RAM total.
---

# Skill: observability:tempo-config

## Input necessário
1. Retenção desejada (default: 48h — dev only)
2. Porta gRPC do Collector que envia traces (default: Collector → Tempo em `tempo:4317`)
3. Grafana já existe no docker-compose? (para adicionar data source)

## O que gerar

### `infra/tempo/tempo.yaml`
```yaml
server:
  http_listen_port: 3200   # Grafana consulta aqui

distributor:
  receivers:
    otlp:
      protocols:
        grpc:
          endpoint: 0.0.0.0:4317  # OTEL Collector envia traces aqui
        http:
          endpoint: 0.0.0.0:4318

ingester:
  max_block_duration: 5m   # flush para storage a cada 5min
  trace_idle_period: 10s

compactor:
  compaction:
    block_retention: 48h   # RETENÇÃO MÁXIMA — dev only, nunca aumentar sem memória

storage:
  trace:
    backend: local
    local:
      path: /var/tempo/traces
    wal:
      path: /var/tempo/wal

# Limites conservadores — 14GB total no sistema
# Tempo deve usar max 512MB
limits_config:
  max_bytes_per_trace: 5_000_000     # 5MB por trace — evitar traces gigantes
  max_search_bytes_per_trace: 50_000  # 50KB para busca
  ingestion_rate_limit_bytes: 5_000_000  # 5MB/s max ingestion

querier:
  search:
    max_result_limit: 100
    default_result_limit: 20
```

### Serviço no `docker-compose.yml`
```yaml
tempo:
  image: grafana/tempo:2.4.1
  command: ["-config.file=/etc/tempo/tempo.yaml"]
  volumes:
    - ./infra/tempo/tempo.yaml:/etc/tempo/tempo.yaml:ro
    - tempo_data:/var/tempo
  ports:
    - "3200:3200"   # HTTP query — Grafana data source
    - "4317:4317"   # gRPC — OTEL Collector envia aqui
  mem_limit: 512m
  memswap_limit: 512m
  healthcheck:
    test: ["CMD", "wget", "-qO-", "http://localhost:3200/ready"]
    interval: 10s
    timeout: 3s
    retries: 5

volumes:
  tempo_data:
    driver: local
    driver_opts:
      type: tmpfs           # RAM disk — sem persistência, adequado para dev
      device: tmpfs
      o: "size=1g"          # máximo 1GB de traces em RAM
```

### Provisioning do data source Grafana (`infra/grafana/datasources/tempo.yaml`)
```yaml
apiVersion: 1

datasources:
  - name: Tempo
    type: tempo
    access: proxy
    url: http://tempo:3200
    uid: tempo
    isDefault: false
    jsonData:
      tracesToLogsV2:
        datasourceUid: loki
        filterByTraceID: true
        filterBySpanID: false
        tags:
          - key: service.name
            value: service
      serviceMap:
        datasourceUid: prometheus
      nodeGraph:
        enabled: true
      search:
        hide: false
      lokiSearch:
        datasourceUid: loki
```

### Query de trace no Grafana (para referência nos links do frontend)
```
# URL pattern para link direto ao trace pelo trace_id:
http://localhost:3000/explore?orgId=1&left={"datasource":"tempo","queries":[{"query":"<TRACE_ID>","queryType":"traceId"}]}

# PromQL para correlacionar Prometheus + Tempo:
# Usar exemplars (se habilitado) ou trace_id nos logs Loki
```

## Checklist pós-geração
- [ ] `block_retention: 48h` — nunca aumentar sem checar RAM disponível
- [ ] `mem_limit: 512m` no docker-compose
- [ ] Volume `tmpfs` com `size=1g` — bounded, sem crescimento infinito em disco
- [ ] Data source `uid: tempo` — usado nos links do frontend
- [ ] `curl http://localhost:3200/ready` retorna 200 após start
- [ ] Trace aparece no Tempo após enviar request instrumentado
- [ ] Grafana Explore: buscar por trace_id do frontend funciona
