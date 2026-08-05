# protocol

Контракт каналу Runner ↔ Cloud. Go-пакет `github.com/roostlabs/protocol`.

Ширший контекст — у `../.github-private/PROJECT_CONTEXT.md` (архітектурні рішення)
і `../.github-private/roadmap.md` (фази). Прочитай перед змінами.

## Правила цього репо

- `PROTOCOL.md` — джерело правди. Змінив типи в Go — онови спец, і навпаки.
- **Zero dependencies.** Пакет споживають і `runner`, і `cloud`; `id`/`ts` передає
  викликач, щоб не тягнути uuid.
- Новий `type` або поле в `data` — **не** breaking. Bump `Version` — breaking.
  Невідомий `type` і невідомі поля ігноруються мовчки, ніколи не reject.
- Вгору не йдуть значення кредів, вміст файлів, секрети. `CredSet.Value` іде лише
  вниз (Managed-режим) — `String()` і `LogValue()` мусять редагувати його завжди.
- Публічний репо — коментарі англійською.
- Версіонується тегами; споживачі залежать від тега, не від `main`.
