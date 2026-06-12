<img width="1440" height="856" alt="image" src="https://github.com/user-attachments/assets/10f65a45-1867-42dc-bb02-d0016f9c21e9" />

# Klyra

Прототип агентного CLI для программирования на Go.

## Запуск локально

```sh
go run . run "inspect this project"
```

Провайдер по умолчанию — `mock`, поэтому команда работает без API-ключей и удобна для тестирования цикла агента и инструментов.

## Провайдер OpenAI

Провайдер `openai` использует современный Responses API. Устаревший путь через Chat Completions также доступен как `chat`.

```sh
export OPENAI_API_KEY="..."
export OPENAI_MODEL="..."
go run . --provider openai run "inspect this project and suggest next steps"
```

Опционально:

```sh
export OPENAI_BASE_URL="https://api.openai.com/v1"
```

Параметры генерации:

```sh
go run . --provider openai --model "$OPENAI_MODEL" --reasoning low --max-output-tokens 2048 run "fix the failing tests"
```

`--store` отключён по умолчанию — локальные сессии остаются без состояния, если явно не включить.

## Локальные модели через Ollama

```sh
export OLLAMA_MODEL="qwen2.5-coder:7b"
go run . --provider ollama run "inspect this project"
```

Опционально:

```sh
export OLLAMA_BASE_URL="http://localhost:11434/v1"
```

Модели Ollama с поддержкой vision могут получать изображения из TUI через `/attach path/to/image.png` в формате `image_url`.

## Провайдер Anthropic

```sh
export ANTHROPIC_API_KEY="..."
export ANTHROPIC_MODEL="claude-sonnet-4-5"
go run . --provider anthropic run "inspect this project"
```

Опционально:

```sh
export ANTHROPIC_BASE_URL="https://api.anthropic.com"
```

## Провайдер Gemini

```sh
export GEMINI_API_KEY="..."
export GEMINI_MODEL="gemini-2.5-flash"
go run . --provider gemini run "inspect this project"
```

Опционально:

```sh
export GEMINI_BASE_URL="https://generativelanguage.googleapis.com/v1beta"
```

## Вспомогательные команды

```sh
go run . config init
go run . config show --profile coding
go run . doctor
go run . tools
go run . instructions
go run . instructions --content
go run . skills --all
go run . skills --query "frontend css" --content
go run . status --diff
go run . checkpoint create before-refactor
go run . checkpoint list
go run . diff preview patch.diff
go run . diff apply patch.diff --yes
go run . policy check "git status --short"
go run . policy check --sandbox read-only "echo hello > file.txt"
go run . sessions
go run . sessions compact feature-work
```

## Интерактивные сессии

```sh
go run . chat --session feature-work
go run . tui --session feature-work
go run . --session feature-work run "continue the refactor"
```

Сессии хранятся в `.agentcli/sessions` в рабочей директории и исключены из git.
Внутри чата `/compact` переписывает старую историю в компактное детерминированное резюме, чтобы будущие запросы тратили меньше контекстных токенов.

TUI на Bubble Tea — основной интерфейс. Поддерживаемые команды: `/help`, `/status`, `/settings`, `/provider`, `/model`, `/reasoning`, `/limits`, `/approval`, `/sandbox`, `/attach`, `/attachments`, `/instructions`, `/skills`, `/compact`, `/clear`, `/exit`.

`F2` или `Ctrl+S` открывает панель настроек. `Tab` — переключение между полями, стрелки влево/вправо — выбор значений, `Enter` — применить.

Пример работы в TUI:

```text
/provider ollama
/model llama3.2-vision
/endpoint http://localhost:11434/v1
/reasoning low
/limits context 16000
/attach screenshots/error.png
explain this screenshot and inspect the relevant code
```

При режиме апрува `ask` рискованные вызовы инструментов показываются как запрос подтверждения. `y`/`Enter` — разрешить, `n`/`Esc` — отклонить. `always` — разрешить всё без запросов.

Вложения изображений отправляются только со следующим запросом и не сохраняются в base64 в истории сессии.

## Режимы и корзина контекста

Klyra имеет реальные ограничения по режимам, а не просто метки:

- `plan` — только чтение и веб-поиск с опциональным `update_plan`; shell, запись, патчи и внешние MCP-инструменты скрыты/заблокированы.
- `inspect` — только чтение; инструменты записи и shell скрыты/заблокированы.
- `edit` — инструменты записи требуют файлов в корзине контекста.
- `repair` — фокусирует агента на падающем выводе, релевантном коде и текущем диффе.
- `refactor` — открывает пути preview/поиска и требует явной корзины контекста перед широкими патчами.

Использование:

```sh
go run . --mode plan run "plan the auth refactor"
go run . --mode inspect run "map the auth flow"
go run . --mode edit --context-file pkg/auth/middleware.go run "fix the auth bug"
```

```text
/mode edit
/cart add pkg/auth/middleware.go pkg/auth/login_test.go
```

После хода отладчик контекста показывает режим, корзину, доступные инструменты и риски.

## Vision

Вложения изображений поддерживаются для OpenAI Responses, Anthropic, Gemini и OpenAI-compatible провайдеров (в том числе Ollama). Вложения кодируются в формате провайдера и удаляются из сохранённой истории после хода.

## Инструкции проекта

Klyra автоматически загружает стандартные файлы инструкций репозитория в системный промпт:

- `AGENTS.md`
- `CLAUDE.md`
- `GEMINI.md`
- `.agentcli/instructions.md`
- `.agentcli/rules.md`
- `.cursorrules`
- `.github/copilot-instructions.md`
- `.cursor/rules/*.md`

`go run . instructions --content` — посмотреть, что именно увидит агент.

## Навыки (Skills)

Навыки — небольшие task-specific markdown-сценарии. Klyra автоматически подбирает их по тексту задачи и путям в корзине, и внедряет только подходящие в системный промпт.

Поддерживаемые расположения:

- `.klyra/skills/*.md`
- `.klyra/skills/*/SKILL.md`
- `.agentcli/skills/*.md`
- `.agentcli/skills/*/SKILL.md`
- `skills/*.md`
- `skills/*/SKILL.md`

Пример метаданных:

```md
name: Frontend Cleanup
description: CSS and UI cleanup rules
triggers: frontend, css, style

Use focused edits and avoid glassmorphism.
```

`go run . skills --all` — список навыков, `go run . skills --query "migration sql" --content` — просмотр совпадений. Отключить: `--no-skills` или `skills=off` в TUI.

## Политика апрува

Рискованные инструменты (`bash`, `write_file`, `diff_patch`, точечные инструменты записи и восстановление чекпоинта) поддерживают режимы апрува:

```sh
go run . --approval ask run "fix the failing tests"
go run . --approval always run "apply the known local fix"
go run . --approval never run "inspect only"
```

Восстановление чекпоинта — явное:

```sh
go run . checkpoint restore before-refactor
```

Предпросмотр диффа — валидация без применения:

```sh
cat patch.diff | go run . diff preview
```

Применение диффа всегда валидирует патч и создаёт чекпоинт:

```sh
go run . diff apply patch.diff
go run . diff apply patch.diff --yes --checkpoint=false
```

Политика shell объясняет, как команда будет обработана:

```sh
go run . policy check "git reset --hard HEAD"
```

Профили песочницы:

```sh
go run . --sandbox read-only run "inspect the project"
go run . --sandbox workspace-write run "fix a typo"
go run . --sandbox danger-full-access run "fetch dependencies"
```

## Маршрутизация моделей

Дешёвые модели для инспекции, мощные для редактирования и глубокого рассуждения:

```sh
go run . --provider openai \
  --stream \
  --max-context-tokens 32000 \
  --max-instruction-bytes 12000 \
  --fast-model "$FAST_MODEL" \
  --edit-model "$CODING_MODEL" \
  --deep-model "$REASONING_MODEL" \
  run "inspect the project and propose next steps"
```

Маршрутизация следует явному режиму агента: `inspect` → быстрый маршрут, `edit`/`repair` → маршрут редактирования, `plan`/`refactor` → глубокий маршрут.

## Компактизация контекста

Агент локально оценивает токены промпта и упаковывает контекст перед вызовами провайдера. Сохраняет системный промпт, оставляет последние ходы, удаляет осиротевшие выводы инструментов и вставляет компактное резюме при превышении `--max-context-tokens`.

Кокпит контекста строит небольшую корзину ретривала перед каждой задачей. Ранжирует фрагменты файлов через BM25, AST repo-map и локальные hash-эмбеддинги по словам, подтокенам идентификаторов и символьным n-граммам — без сетевого сервиса эмбеддингов.

## Реализованные инструменты

- `discover_tools` — открывает группы возможностей (`workspace`, `edit`, `git`, `shell`, `web`, `plan`, `external`) для текущего запуска.
- `guide` — возвращает компактное task-specific руководство по запросу.
- `project_map` — карта репозитория с бюджетом токенов; включает важные файлы и AST-символы.
- `list_files` — список файлов рабочей директории, пропуская сгенерированные папки.
- `read_file` — чтение файлов с нарезкой по строкам.
- `file_outline` — компактный список импортов/символов одного файла.
- `read_symbol` — чтение одного AST-символа вместо целого файла.
- `read_go_symbol` — чтение Go-объявления по имени символа.
- `create_file` — создание только новых файлов.
- `replace_symbol`, `replace_lines`, `insert_lines` — точечные инструменты редактирования существующих файлов.
- `write_file` — устаревший полнофайловый writer; скрыт в нормальных промптах редактирования.
- `search` — поиск через `rg`.
- `web_search`, `fetch_url` — поиск в интернете и загрузка страниц.
- `update_plan` — запись короткого структурированного плана для режима plan.
- `bash` — выполнение shell-команд с таймаутом и сжатием вывода.
- `diff_patch` — применение unified diff через `git apply`.

## Проверка

```sh
go test ./...
go build ./...
```

---

## Наши улучшения

Форк добавляет 8 улучшений поверх оригинала:

### 1. Параллельные вызовы инструментов
Если LLM запрашивает несколько инструментов за один шаг (например, прочитать три файла), они выполняются конкурентно через горутины. Проверки на дубли и апрув остаются последовательными, результаты добавляются в контекст в оригинальном порядке.

### 2. Retry с экспоненциальным backoff для LLM
При ошибках 429, 502, 503, 504, rate limit или разрыве соединения агент автоматически повторяет запрос до 3 раз с задержками 1с → 2с → 4с. Уважает отмену контекста.

### 3. Обрезка длинных выводов инструментов
Вывод каждого инструмента обрезается до 32 КБ перед записью в контекст. Защищает окно контекста от переполнения при больших выводах bash или файловых операций.

### 4. Провайдер Ollama
Новый `pkg/llm/ollama.go` — тонкая обёртка над OpenAI-compatible API. Подключается к локальному серверу Ollama по умолчанию на `localhost:11434`. Поддерживает переменную окружения `OLLAMA_HOST`.

```go
provider, err := llm.NewOllamaProviderFromEnv()
// или явно:
provider, err := llm.NewOllamaProvider("http://localhost:11434/v1")
```

### 5. Суб-агенты
Новый инструмент `sub_agent` позволяет агенту делегировать изолированные подзадачи дочерним агентам. Дочерний агент получает собственный контекст и набор инструментов, результат возвращается как вывод инструмента. Рекурсия заблокирована.

```go
cfg := agent.Config{
    SubAgentFactory: agent.DefaultSubAgentFactory(cfg),
}
```

### 6. Калибровка подсчёта токенов
`BudgetedWindow` сравнивает оценочное количество токенов с реальным из ответа API и корректирует внутренний коэффициент через EMA (α=0.3). Предотвращает переполнение контекстного окна при систематическом занижении оценки.

### 7. Корректная обработка Ctrl+C
Проверка `ctx.Done()` добавлена в начале каждого шага, перед запуском горутин и после их завершения. Агент завершается немедленно при отмене контекста.

### 8. Структурированные логи (slog)
В `Config` добавлено поле `Logger *slog.Logger` (по умолчанию `slog.Default()`). Агент логирует вызовы инструментов, ошибки, политику, retry и токены в структурированном формате.

```go
logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
cfg := agent.Config{
    Logger: logger,
}
```

### Изменённые файлы

| Файл | Что изменилось |
|------|----------------|
| `pkg/agent/agent.go` | Параллельные tool calls, retry, обрезка выводов, Ctrl+C, slog |
| `pkg/agent/subagent.go` | Новый файл: инструмент `sub_agent` и `SubAgentFactory` |
| `pkg/llm/ollama.go` | Новый файл: провайдер Ollama |
| `pkg/context/window.go` | Калибровка токенов через `CalibrateFrom()` |
| `pkg/tools/registry.go` | Регистрация `sub_agent` в реестре инструментов |
