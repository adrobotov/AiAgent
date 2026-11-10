# ROADMAP — План развития AI Agent

> Составлен по результатам анализа кодовой базы (ноябрь 2026).
> Текущее состояние: проект собирается, `go vet` и `gofmt` чистые, `go test ./... -race` проходит во всех пакетах.

---

## 1. Диагностика текущего состояния

### Что уже есть (Фазы 1–2 из README)
- GigaChat API + OAuth с автообновлением токенов (`auth/`, `llm/`)
- Function calling через `tools.Manager` (`tools/`)
- Streaming, Vision, Web Search, Code Interpreter
- SQLite-история с разговорами (`history/`)
- Markdown-рендеринг, CLI с командами `/stream`, `/vision`, `/conversations` и др.
- Новые (не закоммиченные): `tools/filesystem.go`, `tools/process.go`, `history/history_test.go`

### Найденные проблемы (по убыванию критичности)

| # | Проблема | Где | Серьёзность |
|---|----------|-----|-------------|
| 1 | **Цикл tool-calling обрывается после первого вызова**: в `Process()` стоит `return` внутри цикла — агент не может выполнять цепочки из нескольких инструментов | `agent/agent.go:111` | 🔴 Критично ✅ исправлено |
| 2 | **История портится**: в `history` сохраняется `rawResult` инструмента как ответ ассистента; сообщения `function` вообще не сохраняются → контекст при продолжении диалога некорректен | `agent/agent.go:107-109` | 🔴 Критично ✅ исправлено |
| 3 | **Переключение разговоров не восстанавливает контекст**: `/switch` меняет ID в SQLite, но `Agent.history` не загружается из `Store.GetMessages` — LLM не видит предыдущий диалог | `cmd/agent/main.go:164-190` | 🔴 Критично ✅ исправлено |
| 4 | **Безопасность**: `FileSystemTool` создаётся с `Root=""` (доступ ко всей файловой системе), `ProcessTool` — без whitelist команд, всё включено по умолчанию без подтверждения пользователя | `tools/tools.go:36-37` | 🔴 Критично ✅ исправлено |
| 5 | `maxTurns` используется для двух разных целей: лимит истории (`len(a.history) >= a.maxTurns`) и лимит итераций цикла — путаница, обрезка захардкожена `-10` | `agent/agent.go:46-47` | 🟠 Высокая ✅ исправлено |
| 6 | Дублирование диспетчеризации инструментов: `ExecuteJSON()` в `agent/agent.go` повторяет логику `tools.Manager` (и рассинхронизировано — нет `filesystem`/`process`) | `agent/agent.go:172-220` | 🟠 Высокая ✅ исправлено |
| 7 | `/stream` не использует tools, не сохраняет ответ в историю, работает с пустой историей (`nil`) | `cmd/agent/main.go:242-268` | 🟠 Высокая ✅ исправлено |
| 8 | `/vision` запрашивает вопрос отдельным `scanner.Scan()` (двойной ввод), ответ не попадает в историю | `cmd/agent/main.go:271-315` | 🟡 Средняя |
| 9 | `structured/` (валидация по JSON Schema) реализован, но **нигде не подключён** | `structured/structured.go` | 🟡 Средняя ✅ исправлено |
| 10 | Нет тестов: `agent`, `llm` (нет mock-сервера), `structured`, `markdown`, `config`, `vision`, `auth` — `no test files` | — | 🟡 Средняя ✅ исправлено |
| 11 | `cmd/agent/main.go` — «god-file» на 456 строк: парсинг команд regex'ами, UI и бизнес-логика перемешаны | `cmd/agent/main.go` | 🟡 Средняя ✅ исправлено (команды в `commands.go`) |
| 12 | Секреты в `config.yaml`; нет поддержки env-переменных (`GIGACHAT_AUTH_KEY` и т.п.) | `config/config.go` | 🟡 Средняя ✅ исправлено (env-переменные) |
| 13 | Калькулятор не поддерживает скобки, унарный минус, степени | `tools/tools.go:234+` | 🟢 Низкая ✅ исправлено |
| 14 | Нет CI, линтера, `Makefile`, трекинга токенов/usage | — | 🟢 Низкая ✅ исправлено |
| 15 | Weather — мок-данные | `tools/tools.go:193-220` | 🟢 Низкая ✅ исправлено (Open-Meteo + wttr.in) |

---

## 2. План развития

### 🔧 Фаза 0 — Техдолг и стабилизация ядра (завершена)

Цель: агент корректно выполняет цепочки инструментов и помнит контекст.

- [x] **0.1. Починить tool-loop** в `agent/agent.go`: цикл продолжается до ответа без `function_call`, результаты инструментов передаются модели обратно.
- [x] **0.2. Исправить запись истории**: user-сообщение и финальный ответ ассистента корректно записываются в `Agent.history` и в SQLite; след function-протокола хранится внутри итерации цикла.
- [x] **0.3. Загрузка истории из SQLite**: метод `Agent.LoadHistory([]llm.Message)`; при `/switch` история загружается через `store.GetMessages` в контекст агента.
- [x] **0.4. Разделить `maxTurns` / `max_history_messages`** — два независимых параметра конфига.
- [x] **0.5. Удалить `ExecuteJSON()`** из `agent/agent.go` — вся диспетчеризация через `tools.Manager`.
- [x] **0.6. Рефакторинг `cmd/agent/main.go`**: команды вынесены в `cmd/agent/commands.go`, таблица команд (`commandTable()`) вместо regex-переключателя.
- [x] **0.7. Конфиг через env**: `AGENT_AUTH_KEY`, `AGENT_BASE_URL`, `AGENT_MODEL`, `AGENT_OAUTH_SCOPE`, `AGENT_SYSTEM_PROMPT`, `AGENT_TEMPERATURE`, `AGENT_MAX_TOKENS`, `AGENT_MAX_TURNS`, `AGENT_MAX_HISTORY` — env перекрывает YAML.
- [x] **0.8. Streaming + tools**: `llm.Stream()` накапливает `function_call` из delta-чанков (`mergeFunctionCalls`), новый `Agent.ProcessStream()` объединяет стрим и tool-loop; `/stream` использует его и сохраняет историю.
- [x] **0.9. Тесты ядра**: `agent/agent_test.go` — 9 тестов с mock-LLM (httptest): цепочка из 2+ function calls, ошибки инструментов, max-turns, обрезка истории, стрим с tools и без.

### 🔒 Фаза 1 — Безопасность и надёжность инструментов (завершена)

- [x] **1.1. Sandbox для инструментов**: секция `tools:` в конфиге (`root`, `allowed_commands`, `confirm`, `max_result_size`); по умолчанию `FileSystemTool.Root` = текущая директория, `ProcessTool` — whitelist `DefaultAllowedCommands` (deny-by-default).
- [x] **1.2. Подтверждение опасных действий**: `Policy`/`Confirmable` в `tools` (write/delete/move/exec/kill → «Approve? [y/N]»), режимы `ask` / `always` / `never` (`tools.confirm`, env `AGENT_TOOLS_CONFIRM`).
- [x] **1.3. Код-интерпретер**: изолированная временная директория (`os.MkdirTemp` + `RemoveAll`), таймауты компиляции (30s) и запуска (10s) через `context`, лимит вывода 10k символов.
- [x] **1.4. Парсер `calc` переписан**: рекурсивный спуск — скобки, унарный минус/плюс, `^` (право-ассоциативный), корректная обработка ошибок.
- [x] **1.5. Реальный weather** через Open-Meteo (геокодинг + текущая погода, WMO-коды), кеш 10 минут, fallback на мок при недоступности API.
- [x] **1.6. Лимиты**: обрезка результата любого инструмента (`Manager.Execute`, `max_result_size`, по умолчанию 20k символов, безопасно по границе рун); web_search уже имел таймаут 10s.
- [x] Тесты фазы 1: `tools/sandbox_test.go` — sandbox root, deny-by-default whitelist, подтверждения (never/ask), обрезка результатов, WMO-коды; `tools_test.go` — новые кейсы калькулятора.

### 🧠 Фаза 2 — Память, контекст и качество (завершена)

- [x] **2.1. Трекинг токенов**: `llm.Usage` парсится из ответа Chat и из SSE-чанков Stream; `Agent.Usage()`/`ResetUsage()`; команда `/usage` в CLI.
- [x] **2.2. Умная обрезка контекста**: лимит по токенам (`context_tokens`, эвристика ~3 символа/токен) + опциональная LLM-суммаризация старой половины истории (`summarize: true`, одна итерация за ход для ограничения латентности).
- [x] **2.3. Подключён `structured/`**: команда `/schema <file> [prompt]` — генерация JSON по схеме + валидация ответа модели (`Validator`), поддержка code-fence stripping.
- [x] **2.4. Долговременная память**: `llm.Embed()` (GigaChat Embeddings API), таблица `memories` в SQLite с BLOB-векторами, cosine-поиск, автоподстановка топ-3 релевантных фактов в system prompt, команды `/remember`, `/memories`, `/forget`; включается `agent.memory: true`.
- [x] **2.5. Покрытие тестами**: новые тесты в `structured`, `config` (defaults/YAML/env-приоритет), `llm` (Chat/Stream/Embed с mock-сервером, merge фрагментов function-call), `markdown`, `auth` (кеш/обновление токена), `history` (memory + cosine), `agent` (usage, суммаризация).
- [x] **2.6. Инфраструктура**: GitHub Actions (build/vet/test -race/gofmt на Linux+Windows, golangci-lint), `Makefile`, `.golangci.yml` (v2 schema).

### 🤖 Фаза 3 — Агенты и автоматизация (завершена)

- [x] **3.1. Плагинная система**: subprocess-протокол в `tools/plugin.go` — плагин возвращает манифест на `--describe` (name/description/parameters) и отвечает JSON на stdin-запрос; защита built-in имён, warnings для сломанных плагинов; конфиг `tools.plugins: [<command>...]`.
- [x] **3.2. Task Planner**: `agent/planner.go` — `Plan()` (генерация нумерованного плана, разбор через `stripNumbering`) + `ExecutePlan()` (каждый шаг видит результаты предыдущих, прогресс-callback, FailedAt при сбое); команда `/plan <task>` с подтверждением.
- [x] **3.3. Multi-agent**: `RunTeam()` — pipeline planner → executor (с tools) → critic (`APPROVED`/`REVISION NEEDED`) с одной ревизией; отказ критика не фатален; команда `/team <task>`.
- [x] **3.4. API Server**: `server/server.go` — `POST /v1/chat` (JSON), `POST /v1/chat/stream` (SSE), `GET /v1/usage`, `GET /healthz`; изолированные сессии по `session_id`; команда `/serve`, порт `server.port`.
- [x] **3.5. Экспорт разговоров**: `/export <id> [markdown|json]` — выгрузка в файл `export_<id>.md`/`.json`.
- [x] Тесты фазы 3: интеграционный тест плагина (сборка helper-exe через `go build`), planner/team с mock-LLM (approved, revision, сбой критика), server (healthz, chat, SSE, usage, изоляция сессий).

### 📦 Фаза 4 — Дистрибуция (завершена)

- [x] **Версионирование**: `main.version` через ldflags, флаг `--version`; Makefile target `build VERSION=v1.0.0`.
- [x] **GoReleaser**: `.goreleaser.yaml` — linux/windows/darwin × amd64/arm64, zip для Windows, checksums, changelog; release job в CI по тегу `v*`.
- [x] **Pure-Go SQLite**: переход с `mattn/go-sqlite3` (cgo) на `modernc.org/sqlite` — кросс-компиляция с `CGO_ENABLED=0`.
- [x] **Examples**: `examples/basic`, `examples/stream`, `examples/server` — использование агента как библиотеки.
- [x] **Godoc**: пакетные доки (`doc.go`) для всех 10 публичных пакетов.
- [x] **CI обновлён**: Go 1.26, матрица Ubuntu/Windows, lint, release по тегу.

---

## 3. Рекомендуемый порядок работ (ближайшие 3 шага)

1. **Фазы 0.1–0.3** — это баги, ломающие базовый сценарий (многошаговые инструменты + память диалога). Оценка: 1–2 дня.
2. **Фаза 1.1–1.2** — до появления filesystem/process в проде нужны sandbox и подтверждения. Оценка: 1–2 дня.
3. **Фаза 0.9 + 2.5** — тесты на исправленный цикл закрепляют результат и ускоряют дальнейший рефакторинг.
