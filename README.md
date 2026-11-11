# AI Agent

Интерактивный CLI AI агент на Go с поддержкой инструментов (tools/functions calling).

## Возможности

- 💬 Интерактивный чат-интерфейс в терминале
- 🧰 Система инструментов (echo, calc, time, weather, web_search, code_interpreter)
- 🤖 Поддержка GigaChat API с OAuth аутентификацией
- 🎨 Markdown рендеринг для красивого вывода
- 🎨 Цветной вывод в терминале
- 🔄 Автоматический retry при ошибках
- 💾 SQLite история диалогов
- 🔐 Автоматическое обновление OAuth-токенов
- ⚙️ Конфигурация через YAML файл
- 📜 История диалога с ограничением контекста
- 🎯 Function calling / Tool calling
- 📡 **Streaming** — потоковая генерация ответов (`/stream`)
- 👁️ **Vision** — анализ изображений (`/vision <path> <question>`)
- 🌐 **Web Search** — поиск в интернете (DuckDuckGo)
- 💻 **Code Interpreter** — выполнение Go кода
- 🗄️ **Database** — выполнение SQL запросов к SQLite
- 🐙 **Git** — операции с git (status, diff, log, commit, push и др.)

## Команды CLI

| Команда | Описание |
|---------|----------|
| `/help` | Показать справку |
| `/clear` | Новая беседа |
| `/exit` | Выйти |
| `/stream` | Режим потоковой генерации (с поддержкой tools) |
| `/vision <path> <question>` | Анализ изображения |
| `/usage` | Расход токенов за сессию |
| `/model [name]` | Показать список моделей или переключить модель |
| `/schema <file> <prompt>` | Генерация JSON по схеме с валидацией |
| `/remember <text>` | Сохранить в долговременную память |
| `/memories` | Список долговременных памятей |
| `/forget` | Удалить все памяти |
| `/conversations` | Список разговоров |
| `/switch <id>` | Переключиться на разговор (контекст восстанавливается) |
| `/plan <task>` | Декомпозиция задачи и пошаговое выполнение |
| `/team <task>` | Multi-agent pipeline: planner → executor → critic |
| `/export <id> [md|json]` | Экспорт разговора в файл |
| `/serve` | Запуск HTTP API сервера |

## Требования

- Go 1.26+

## Установка

```bash
# Установить зависимости
go mod tidy

# Собрать проект (с версией)
go build -ldflags "-X main.version=v1.0.0" -o aiagent ./cmd/agent

# Или через Makefile
make build VERSION=v1.0.0

# Проверить версию
./aiagent --version
```

## Примеры использования как библиотеки

```bash
go run ./examples/basic  "What is 2 + 2?"   # один запрос → ответ
go run ./examples/stream "Tell me a story"  # стриминг ответа
go run ./examples/server                     # HTTP API на :8080
```

## Настройка

### Шаг 1: Регистрация и получение ключа авторизации

1. Зарегистрируйтесь на [developers.sber.ru](https://developers.sber.ru/)
2. Перейдите в [Личный кабинет GigaChat API](https://developers.sber.ru/studio/workspaces/my-space/get/gigachat-api)
3. Создайте новый проект или используйте существующий
4. Скопируйте **Ключ авторизации** (Authorization key — Base64-encoded Client ID:Client Secret)

### Шаг 2: Настройка конфигурации

Отредактируйте `config.yaml`:

```yaml
llm:
  base_url: https://api.giga.chat/v1
  model: GigaChat-2-Max  # или GigaChat-2, GigaChat-2-Pro, GigaChat-3-Ultra
  auth_key: "ваш_ключ_авторизации"  # Ключ из личного кабинета
  oauth_scope: GIGACHAT_API_PERS  # GIGACHAT_API_PERS (физлица), GIGACHAT_API_B2B или GIGACHAT_API_CORP
  oauth_rquid: ""  # Опционально: фиксированный RqUID, если пустой — генерируется UUID4
  temperature: 0.7
  max_tokens: 4096
```

### Шаг 3: Запуск

```bash
go run ./cmd/agent
```

**Как это работает:**
- Агент автоматически получает Bearer-токен через OAuth
- Токен действует 30 минут и обновляется автоматически
- API точка входа: `https://api.giga.chat/v1/chat/completions`
- OAuth точка: `https://ngw.devices.sberbank.ru:9443/api/v2/oauth`
- Обязательный заголовок `RqUID` (UUID4) генерируется автоматически
- Поддерживаемые scope: `GIGACHAT_API_PERS`, `GIGACHAT_API_B2B`, `GIGACHAT_API_CORP`

## Использование

```bash
# Запуск
go run ./cmd/agent

# Или после сборки
./agent
```

### Команды внутри чата

| Команда      | Описание                    |
|-------------|-----------------------------|
| `/help`     | Показать справку            |
| `/clear`    | Очистить историю диалога    |
| `/exit`     | Выйти из агента             |

## Встроенные инструменты

Агент поддерживает следующие инструменты:

- **echo** — возвращает отправленное сообщение
- **calc** — вычисляет выражения: скобки, унарный минус, `^` (степень)
- **time** — возвращает текущую дату и время
- **weather** — реальная погода через [Open-Meteo](https://open-meteo.com/) (без ключа), кеш 10 минут
- **filesystem** — операции с файлами в sandbox-директории (read/write/list/…)
- **process** — выполнение внешних команд из whitelist
- **web_search** — поиск через DuckDuckGo
- **code_interpreter** — выполнение Go кода в изолированной временной директории

### Безопасность инструментов

Секция `tools` в `config.yaml`:

```yaml
tools:
  root: ""                    # sandbox-директория для filesystem (пусто = текущая директория)
  allowed_commands:           # whitelist для process (пустой список = запрет всего)
    - go
    - git
    - python
  confirm: ask                # ask (спрашивать y/N) | always (разрешать) | never (запрещать)
  max_result_size: 20000      # обрезка результатов инструментов (символы)
```

Опасные операции (`write`, `delete`, `move` в filesystem; `exec`, `kill` в process) при режиме
`ask` требуют подтверждения: `Approve? [y/N]`. Env-переменные: `AGENT_TOOLS_ROOT`,
`AGENT_TOOLS_CONFIRM`, `AGENT_TOOLS_MAX_RESULT`.

### Контекст и долговременная память

Секция `agent` дополнительно поддерживает:

```yaml
agent:
  context_tokens: 8000   # примерный бюджет токенов для истории сессии
  summarize: false       # LLM-суммаризация старых сообщений при превышении бюджета
  memory: false          # долговременная память (Embeddings API + SQLite)
```

Env-переменные: `AGENT_CONTEXT_TOKENS`, `AGENT_SUMMARIZE`, `AGENT_MEMORY` (`1`/`true`/`yes`).
При включённой памяти релевантные факты автоматически подставляются в системный промпт.

### Плагины (внешние инструменты)

Секция `tools.plugins` — список внешних команд, реализующих subprocess-протокол:

```yaml
tools:
  plugins:
    - path/to/my-plugin.exe
    - my-plugin.sh
```

Протокол плагина:
- `my-plugin --describe` → JSON-манифест: `{"name", "description", "parameters"}`
- Запрос на stdin: `{"args": {...}}` → ответ на stdout: `{"result": "...", "error": ""}`

Имена плагинов не могут перезаписывать встроенные инструменты.

### HTTP API Server

Команда `/serve` запускает HTTP API (порт `server.port`, по умолчанию 8080):

| Endpoint | Описание |
|----------|----------|
| `POST /v1/chat` | `{"message": "...", "session_id": "..."}` → `{"response", "usage"}` |
| `POST /v1/chat/stream` | То же, но SSE-стриминг (`delta`/`done`/`error` события) |
| `GET /v1/usage` | Расход токенов сессии |
| `GET /healthz` | Проверка живости |

## Архитектура

```
AiAgent/
├── cmd/agent/        # CLI с цветами и анимацией
├── config/           # Конфигурация
├── llm/              # Клиент GigaChat API (streaming)
├── auth/             # OAuth аутентификация
├── agent/            # Ядро с retry логикой
├── tools/            # Инструменты (web_search, code_interpreter)
├── history/          # SQLite история
├── markdown/         # Markdown рендеринг
├── vision/           # Vision (изображения)
├── structured/       # Structured output (JSON schema)
├── config.yaml       # Файл конфигурации
├── go.mod
└── go.sum
```

## Фазы разработки

### ✅ Фаза 1 — Стабильность и UX (завершена)
- ✅ OAuth аутентификация GigaChat
- ✅ Function calling (tools)
- ✅ Калькулятор, время, погода
- ✅ Markdown рендеринг
- ✅ Цветной вывод в терминале
- ✅ Retry логика при ошибках
- ✅ SQLite история диалогов
- ✅ Улучшенный CLI

### ✅ Фаза 2 — Расширение возможностей (завершена)
- ✅ **Streaming** — потоковая генерация ответов
- ✅ **Vision** — поддержка изображений
- ✅ **Structured Output** — JSON schema валидация
- ✅ **Web Search** — поиск через DuckDuckGo
- ✅ **Code Interpreter** — выполнение Go кода

### ✅ Фаза 3 — Агенты и автоматизация (завершена)
- ✅ **Multi-agent** — pipeline planner → executor → critic (`/team`)
- ✅ **Task Planner** — декомпозиция и пошаговое выполнение (`/plan`)
- ✅ **Memory** — долговременная память на embeddings (`/remember`)
- ✅ **Plugins** — внешние инструменты через subprocess-протокол
- ✅ **API Server** — HTTP API с SSE-стримингом (`/serve`)

### ✅ Фаза 4 — Дистрибуция (завершена)
- ✅ Версионирование `--version` через ldflags
- ✅ GoReleaser (linux/windows/darwin, amd64/arm64) + release в CI
- ✅ Примеры библиотечного использования (`examples/`)
- ✅ Godoc для всех пакетов
- ✅ Pure-Go SQLite — кросс-компиляция без cgo
- ✅ Выбор модели в рантайме (`/model`: `GET /models` + встроенный список)
- ✅ Weather с резервным источником (Open-Meteo → wttr.in → мок)

## GigaCode Integration

Проект интегрирован с GigaCode для удобной разработки.

### Команды

Вызовите через `/command-name` в GigaCode:

| Команда    | Описание                                     |
|-----------|----------------------------------------------|
| `/build`  | Собрать все пакеты проекта Go                |
| `/run`    | Запустить AI агента из командной строки       |
| `/test`   | Запустить тесты и создать недостающие         |
| `/clean`  | Очистить бинарные файлы и кеш                |

### Агенты

Доступны через `@agent-name` в GigaCode:

| Агент      | Описание                                     |
|-----------|----------------------------------------------|
| `@code`    | Основной агент для разработки Go-кода         |
| `@reviewer`| Проверка качества кода и лучших практик       |

### Конфигурация

Файл `.gigacode_vsc/gigacode.json` содержит:
- Модель по умолчанию
- Разрешения на инструменты (bash, edit, read)
- Правила контроля доступа к файлам

### `.gigacodeignore`

Определяет файлы, недоступные для чтения агентом:
- `config.yaml` — файл с API ключами
- `*.env` — переменные окружения
- `*.exe` — скомпилированные бинарные файлы

## Лицензия

MIT

## Создание релиза

### Автоматический релиз через Git Tag

```bash
# Linux/macOS
./scripts/create-release.sh v1.0.0

# Windows (PowerShell)
.\scripts\create-release.ps1 v1.0.0

# Или через Makefile
make tag-release VERSION=v1.0.0
```

Скрипт:
1. Собирает проект
2. Создаёт git tag
3. Пушит тег в remote
4. GoReleaser в CI автоматически создаст GitHub Release с артефактами

### Ручной релиз

```bash
# Создаём тег
git tag -a v1.0.0 -m "Release v1.0.0"
git push origin v1.0.0

# GoReleaser в CI создаст release автоматически
```

### Артефакты релиза

GoReleaser создаст:
- **tar.gz** (linux/darwin) / **zip** (windows) с бинарником и документацией
- **checksums.txt** с SHA256 суммами
- **source archive** с исходным кодом
- **Homebrew formula** (опционально)

Проверить артефакты: https://github.com/{owner}/{repo}/releases
