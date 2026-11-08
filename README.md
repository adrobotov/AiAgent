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

## Команды CLI

| Команда | Описание |
|---------|----------|
| `/help` | Показать справку |
| `/clear` | Новая беседа |
| `/exit` | Выйти |
| `/stream` | Режим потоковой генерации |
| `/vision <path> <question>` | Анализ изображения |

## Требования

- Go 1.22+

## Установка

```bash
# Установить зависимости
go mod tidy

# Собрать проект
go build ./cmd/agent
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
- **calc** — вычисляет математические выражения
- **time** — возвращает текущую дату и время
- **weather** — показывает погоду для указанного города (моковые данные)

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

### 🟠 Фаза 3 — Агенты и автоматизация
- [ ] **Multi-agent** — система с несколькими агентами
- [ ] **Task Planner** — планирование сложных задач
- [ ] **Memory** — долговременная память (embeddings)
- [ ] **Plugins** — система плагинов для расширения
- [ ] **API Server** — HTTP API для интеграции

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
