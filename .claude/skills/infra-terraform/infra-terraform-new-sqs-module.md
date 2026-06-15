---
name: infra-terraform:new-sqs-module
description: Cria módulo Terraform completo para SQS com DLQ, política de redriving, visibilidade configurável e outputs necessários. Use sempre que adicionar uma nova fila ao sistema.
---

# Skill: infra-terraform:new-sqs-module

## Input necessário
1. Nome da fila (ex: `orders-confirmed`, `payments-processed`)
2. Serviço consumidor (para documentação)
3. `visibility_timeout` em segundos (default: 30)
4. `max_receive_count` antes de ir pra DLQ (default: 3)

## O que gerar

### `infra/modules/sqs/main.tf`
```hcl
resource "aws_sqs_queue" "dlq" {
  name                      = "${var.queue_name}-dlq"
  message_retention_seconds = var.dlq_retention_seconds

  tags = {
    Environment = var.environment
    ManagedBy   = "terraform"
  }
}

resource "aws_sqs_queue" "main" {
  name                       = var.queue_name
  visibility_timeout_seconds = var.visibility_timeout
  message_retention_seconds  = 3600  # 1h — não reter mensagens velhas

  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.dlq.arn
    maxReceiveCount     = var.max_receive_count
  })

  tags = {
    Environment = var.environment
    ManagedBy   = "terraform"
  }
}
```

### `infra/modules/sqs/variables.tf`
```hcl
variable "queue_name" {
  type        = string
  description = "Nome da fila SQS"
}

variable "environment" {
  type    = string
  default = "dev"
}

variable "visibility_timeout" {
  type    = number
  default = 30
}

variable "max_receive_count" {
  type    = number
  default = 3
}

variable "dlq_retention_seconds" {
  type    = number
  default = 86400  # 24h para DLQ
}
```

### `infra/modules/sqs/outputs.tf`
```hcl
output "queue_url" {
  value = aws_sqs_queue.main.url
}

output "queue_arn" {
  value = aws_sqs_queue.main.arn
}

output "dlq_url" {
  value = aws_sqs_queue.dlq.url
}

output "dlq_arn" {
  value = aws_sqs_queue.dlq.arn
}
```

### Uso no `infra/envs/dev/main.tf`
```hcl
module "orders_confirmed_queue" {
  source     = "../../modules/sqs"
  queue_name = "orders-confirmed"
  environment = "dev"
  visibility_timeout = 30
}

# Passar URL para o serviço no docker-compose via env ou outputs
output "orders_confirmed_queue_url" {
  value = module.orders_confirmed_queue.queue_url
}
```

## Checklist pós-geração
- [ ] `terraform validate` sem erros
- [ ] `terraform plan` revisado — apenas recursos SQS e DLQ
- [ ] Queue URL adicionada ao env do consumer no docker-compose
- [ ] DLQ monitorada — alerta no Prometheus se mensagens > 0
- [ ] `visibility_timeout` >= tempo máximo de processamento do consumer
