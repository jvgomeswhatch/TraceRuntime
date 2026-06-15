---
name: docker-compose:new-service
description: Gera o bloco docker-compose completo para um novo microsserviço com mem_limit, cpus, healthcheck, depends_on e networks. Nunca esquece um campo de segurança ou recurso.
---

# Skill: docker-compose:new-service

## Input necessário
1. Nome do serviço (ex: `payment-service`)
2. Porta HTTP do serviço
3. Depende de: LocalStack? PostgreSQL? Ambos?
4. É Go service ou Python?
5. Perfil (profile): `core`, `ai`, `observability`?

## Template Go Service
```yaml
payment-service:
  build:
    context: ./services/payment-service
    dockerfile: Dockerfile
  container_name: payment-service
  restart: on-failure:3
  profiles: ["core", "full"]
  networks:
    - backend
  environment:
    - SQS_ENDPOINT=http://localstack:4566
    - QUEUE_URL=http://localstack:4566/000000000000/orders-confirmed
    - DB_URL=postgres://app:secret@postgres:5432/platform?sslmode=disable
    - LOG_LEVEL=info
  depends_on:
    localstack:
      condition: service_healthy
    postgres:
      condition: service_healthy
  healthcheck:
    test: ["CMD", "wget", "-qO-", "http://localhost:8080/health"]
    interval: 10s
    timeout: 5s
    retries: 3
    start_period: 15s
  deploy:
    resources:
      limits:
        memory: 128M
        cpus: "0.50"
      reservations:
        memory: 64M
  security_opt:
    - no-new-privileges:true
  read_only: true
  tmpfs:
    - /tmp
```

## Template Python Service
```yaml
python-ai:
  build:
    context: ./services/python-ai
    dockerfile: Dockerfile
  container_name: python-ai
  restart: on-failure:3
  profiles: ["ai", "full"]
  networks:
    - backend
  environment:
    - OLLAMA_BASE_URL=http://ollama:11434
    - LOG_LEVEL=info
  depends_on:
    ollama:
      condition: service_healthy
  healthcheck:
    test: ["CMD", "curl", "-f", "http://localhost:8000/health"]
    interval: 15s
    timeout: 10s
    retries: 3
    start_period: 20s
  deploy:
    resources:
      limits:
        memory: 512M
        cpus: "1.00"
      reservations:
        memory: 256M
  security_opt:
    - no-new-privileges:true
```

## Checklist pós-geração
- [ ] `mem_limit` definido e coerente com a tabela de limites
- [ ] `healthcheck` testa endpoint real (não apenas `echo ok`)
- [ ] `depends_on` com `condition: service_healthy` (não apenas nome)
- [ ] `security_opt: no-new-privileges:true` presente
- [ ] `profiles` correto para o serviço
- [ ] Networks: `backend` para serviços Go/Python, nunca `default`
- [ ] Somar RAM ao total: deve ficar abaixo de 8GB no profile `full`
