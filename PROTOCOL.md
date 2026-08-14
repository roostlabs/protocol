# Roost Channel Protocol — Draft v0.1

Контракт каналу між **Runner** (VPS дева) і **Cloud API** (дашборд). Цей документ — джерело правди для обох сторін; живе в публічному репо `protocol`.

## 1. Транспорт

- **WebSocket over TLS** (`wss://`). З'єднання **завжди ініціює Runner** (outbound-only).
- Один Runner = одне постійне з'єднання. Усі потоки (задачі, метрики, чат, команди) мультиплексуються в ньому.
- Серіалізація: **JSON**, UTF-8, одне повідомлення = один WebSocket text frame.
- Keepalive: ping/pong кожні 30с; 2 пропущені pong → сторона вважає з'єднання мертвим.

## 2. Envelope (спільна обгортка)

Кожне повідомлення в обох напрямках:

```json
{
  "v": 1,                    // версія протоколу
  "id": "uuid",              // унікальний id повідомлення
  "ts": 1754400000000,       // unix ms
  "type": "task.log",        // тип (namespace.action)
  "taskId": "T-42",          // опційно: до якої задачі належить
  "seq": 17,                 // опційно: порядок у межах taskId
  "data": { }                // payload, залежить від type
}
```

Правила:

- Невідомий `type` — **ігнорувати мовчки** (forward compatibility).
- Невідомі поля в `data` — ігнорувати.
- `seq` монотонний у межах `taskId` — дає впорядкування подій без довіри до годинника.

## 3. Handshake і автентифікація

1. Runner відкриває `wss://api.<host>/channel`.
2. Перше повідомлення — завжди `hello` від Runner:

```json
{ "v":1, "type":"hello", "data":{
  "token": "<runner-token з дашборду>",
  "runnerVersion": "0.3.1",
  "protoVersions": [1],
  "host": { "os":"linux", "arch":"amd64", "docker":"27.0" }
}}
```

3. Cloud відповідає `hello.ok` (обрана версія протоколу, серверний час) або `hello.err` (invalid token / unsupported version) і закриває з'єднання.

```json
{ "v":1, "type":"hello.ok", "data":{ "proto":1, "serverTs":..., "runnerId":"r-abc" } }
```

**Version negotiation:** Runner надсилає список підтримуваних версій, Cloud обирає найвищу спільну. Cloud зобов'язаний підтримувати N-1 (open-source Runner'и оновлюються повільно).

## 4. Повідомлення Runner → Cloud (вгору)

| type | призначення | ключові поля `data` |
|---|---|---|
| `hello` | handshake | див. §3 |
| `status` | стан Runner'а (періодично + при зміні) | `state: idle\|busy`, `activeTasks[]`, `queuedTasks` |
| `metrics` | телеметрія хоста/контейнерів (кожні ~5с, коли є підписник) | `host:{cpu,mem,disk,load}`, `sandboxes:[{taskId,cpuPct,memMb}]` |
| `task.event` | подія трейсу виконання (append-only) | `event: stage\|agent_step\|cmd_start\|cmd_output\|cmd_exit\|llm_call\|pr\|error` + payload події |
| `task.state` | зміна стану задачі | `state: queued\|preparing\|running\|awaiting_approval\|done\|failed\|cancelled`, `reason?` |
| `task.result` | фінал задачі | `prUrl?`, `costUsd`, `tokens:{in,out}`, `durationMs` |
| `cred.status` | які креди налаштовані (тільки прапорці!) | `{git:true, taskManager:false, llm:true}` |
| `repo.status` | стан підготовлених реп | `[{repo, branch, lastFetch, dirty}]` |
| `chat` | повідомлення від агента до дева | `taskId`, `text` |
| `query.result` | відповідь на `query` від Cloud | `queryId`, `ok`, `data\|error` |

**Заборонено вгору:** значення кредів, вміст файлів репи, будь-які секрети. `cmd_output` перед відправкою проходить redaction-фільтр (маскування відомих токенів з env).

## 5. Повідомлення Cloud → Runner (вниз)

| type | призначення | ключові поля `data` |
|---|---|---|
| `hello.ok` / `hello.err` | handshake | див. §3 |
| `task.run` | запустити задачу | `taskId`, `ticket:{provider,id,url?,title?,body?}`, `repo`, `budgetUsd?`, `timeoutMs?` |
| `task.cancel` | зупинити задачу (вбити sandbox) | `taskId`, `reason` |
| `task.approve` | апрув кроку (human-in-the-loop) | `taskId`, `stepId`, `approved: bool` |
| `repo.prepare` | клонувати/оновити репу | `url`, `branch?` |
| `cred.set` | **тільки Managed-режим**: записати кред у локальний конфіг | `key: git\|taskManager\|llm`, `value` (relay-only, не логується) |
| `chat` | повідомлення від дева до агента | `taskId`, `text` |
| `query` | одноразовий запит (історія, диск, конфіг) | `queryId`, `what: task_history\|task_trace\|disk_usage\|config`, `params` |
| `subscribe` / `unsubscribe` | вкл/викл потік `metrics` (щоб не слати даремно) | `stream: metrics` |
| `runner.update` | запросити self-update | `version` |

## 6. Семантика reconnect

- Runner перепідключається з exponential backoff (1s → 60s max, jitter).
- Події `task.event` при обриві **буферизуються локально** (вони й так пишуться в SQLite event-store) і дозаливаються після reconnect: Cloud у `hello.ok` повертає `lastSeq` по активних задачах, Runner шле все, що новіше.
- Команди вниз при офлайн-Runner'і: Cloud відповідає дашборду `runner_offline` — **не** зберігає чергу команд (крім явно позначених `queued:true`, TTL 10 хв).
- Задачі **переживають обрив каналу**: sandbox продовжує працювати без зв'язку з Cloud; канал — це спостереження/керування, не життєзабезпечення.

## 7. Помилки

```json
{ "v":1, "type":"error", "data":{ "code":"TASK_NOT_FOUND", "ref":"<id повідомлення-причини>", "msg":"..." } }
```

Коди: `AUTH_FAILED`, `UNSUPPORTED_VERSION`, `TASK_NOT_FOUND`, `BUSY` (concurrency=1, задача в черзі), `BUDGET_EXCEEDED`, `CRED_MISSING`, `INTERNAL`.

## 8. Версіонування протоколу

- `v` в envelope = major. Breaking change → v+1.
- Додавання нових `type` або полів у `data` — **не** breaking (обидві сторони ігнорують невідоме).
- Deprecation: тип позначається deprecated у цьому документі ≥1 major-версію до видалення.

## 9. Відкриті питання

- [ ] Чи потрібен binary frame для великих `cmd_output` чанків (зараз JSON+base64 досить)?
- [ ] E2E-шифрування `cred.set` ключем Runner'а (щоб Cloud технічно не міг прочитати) — v2?
- [ ] Rate limits на `task.event` (флуд-захист від зацикленого агента) — на боці Runner чи Cloud?
- [ ] Формат `task_trace` пагінації для довгих задач.
