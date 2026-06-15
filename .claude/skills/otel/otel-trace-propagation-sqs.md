---
name: otel:trace-propagation-sqs
description: Implementa injeção e extração de W3C traceparent em MessageAttributes SQS — tanto em Go (producer e consumer) quanto em Python (boto3). Garante que spans de consumer sejam filhos do span de producer, mantendo o trace contínuo.
---

# Skill: otel:trace-propagation-sqs

## Input necessário
1. Linguagem: Go (producer e/ou consumer) ou Python (boto3)?
2. Nome da fila SQS
3. Já existe código de send/receive para adaptar, ou scaffold do zero?

## O que gerar

### Go — `internal/tracing/sqs_propagation.go`
```go
package tracing

import (
    "context"

    "github.com/aws/aws-sdk-go-v2/aws"
    "github.com/aws/aws-sdk-go-v2/service/sqs/types"
    "go.opentelemetry.io/otel"
)

// sqsCarrier adapta MessageAttributes como TextMapCarrier W3C
type sqsCarrier map[string]string

func (c sqsCarrier) Get(key string) string  { return c[key] }
func (c sqsCarrier) Set(key, val string)    { c[key] = val }
func (c sqsCarrier) Keys() []string {
    keys := make([]string, 0, len(c))
    for k := range c {
        keys = append(keys, k)
    }
    return keys
}

// InjectToSQSAttrs injeta traceparent no map de MessageAttributes.
// Chamar imediatamente antes de sqs.SendMessage.
func InjectToSQSAttrs(ctx context.Context, attrs map[string]types.MessageAttributeValue) {
    carrier := sqsCarrier{}
    otel.GetTextMapPropagator().Inject(ctx, carrier)

    for k, v := range carrier {
        val := v
        attrs[k] = types.MessageAttributeValue{
            DataType:    aws.String("String"),
            StringValue: &val,
        }
    }
}

// ExtractFromSQSAttrs extrai traceparent e retorna context enriquecido.
// Chamar no início do consumer, antes de criar qualquer span filho.
func ExtractFromSQSAttrs(ctx context.Context, attrs map[string]types.MessageAttributeValue) context.Context {
    carrier := sqsCarrier{}
    for k, v := range attrs {
        if v.StringValue != nil {
            carrier[k] = *v.StringValue
        }
    }
    return otel.GetTextMapPropagator().Extract(ctx, carrier)
}
```

### Go — uso no producer
```go
func (p *Producer) Send(ctx context.Context, payload []byte) error {
    ctx, span := tracer.Start(ctx, "sqs.send.events-queue")
    defer span.End()

    attrs := make(map[string]types.MessageAttributeValue)
    tracing.InjectToSQSAttrs(ctx, attrs) // injeta traceparent

    _, err := p.sqs.SendMessage(ctx, &sqs.SendMessageInput{
        QueueUrl:          aws.String(p.queueURL),
        MessageBody:       aws.String(string(payload)),
        MessageAttributes: attrs,
    })
    if err != nil {
        span.RecordError(err)
        return fmt.Errorf("sqs send: %w", err)
    }
    return nil
}
```

### Go — uso no consumer
```go
func (c *Consumer) process(ctx context.Context, sqsMsg types.Message) {
    // PRIMEIRO: extrair contexto antes de criar span
    ctx = tracing.ExtractFromSQSAttrs(ctx, sqsMsg.MessageAttributes)

    ctx, span := tracer.Start(ctx, "sqs.process.events-queue")
    defer span.End()

    // span agora é filho do span do producer — trace contínuo
    var msg Message
    if err := json.Unmarshal([]byte(*sqsMsg.Body), &msg); err != nil {
        span.RecordError(err)
        return
    }

    span.SetAttributes(
        attribute.String("event.type", msg.EventType),
        attribute.String("trace_id", msg.TraceID), // trace_id do envelope de negócio
    )

    // ...processar...
}
```

### Python — `app/tracing/sqs_propagation.py`
```python
from opentelemetry import trace, propagate
from opentelemetry.propagators.textmap import CarrierT
from typing import Dict, Optional

class SQSCarrier:
    """Adapta MessageAttributes do SQS (boto3) como carrier W3C."""

    def __init__(self, attrs: Optional[Dict] = None):
        self._attrs = attrs or {}

    def get(self, key: str) -> Optional[str]:
        attr = self._attrs.get(key)
        if attr and attr.get("DataType") == "String":
            return attr.get("StringValue")
        return None

    def keys(self):
        return list(self._attrs.keys())

    def set(self, key: str, value: str):
        self._attrs[key] = {"DataType": "String", "StringValue": value}

    @property
    def attrs(self) -> Dict:
        return self._attrs


def inject_to_sqs(ctx) -> Dict:
    """
    Retorna MessageAttributes dict com traceparent injetado.
    Passar o resultado como MessageAttributes no boto3 send_message.
    """
    carrier = SQSCarrier()
    propagate.inject(carrier, context=ctx)
    return carrier.attrs


def extract_from_sqs(message_attributes: Dict):
    """
    Extrai trace context de MessageAttributes SQS.
    Retorna context enriquecido para usar como parent de spans.
    """
    carrier = SQSCarrier(message_attributes)
    return propagate.extract(carrier)
```

### Python — uso no consumer boto3
```python
import boto3
from opentelemetry import trace
from app.tracing.sqs_propagation import extract_from_sqs

tracer = trace.get_tracer("ai-runtime")

def process_message(message: dict):
    attrs = message.get("MessageAttributes", {})
    ctx = extract_from_sqs(attrs)

    with tracer.start_as_current_span("sqs.process", context=ctx) as span:
        body = json.loads(message["Body"])
        span.set_attribute("event.type", body.get("event_type", "unknown"))
        # ...
```

## Gotchas críticos
- SQS limita `MessageAttributes` a **10 entradas** — W3C traceparent usa apenas 1 (`traceparent`)
- ReceiveMessage deve incluir `MessageAttributeNames=["All"]` ou `["traceparent"]` explicitamente — sem isso, attrs não chegam
- Em LocalStack, MessageAttributes funcionam igual à AWS real — sem workaround necessário

## Checklist pós-geração
- [ ] `ReceiveMessage` inclui `MessageAttributeNames: []string{"All"}` no Go
- [ ] `receive_message` inclui `MessageAttributeNames=["All"]` no Python
- [ ] `ExtractFromSQSAttrs` chamado ANTES de `tracer.Start` no consumer
- [ ] `InjectToSQSAttrs` chamado DENTRO do span do producer
- [ ] Trace aparece como trace único no Tempo (producer + consumer no mesmo trace)
