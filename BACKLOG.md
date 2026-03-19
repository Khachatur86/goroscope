# Goroscope — Бэклог задач

> Дата: 2026-03-19 | Версия проекта: 0.1.0
> Источники: анализ кодовой базы + Go Developer Survey 2024-2025 + веб-ресёрч экосистемы

---

## Методология

Каждая задача содержит: приоритет (P0 критично → P3 nice-to-have), категорию, обоснование из ресёрча, и ссылку на gap между текущей реализацией и потребностями гоферов.

---

## Что уже реализовано

> Обновлено: 2026-03-19 после аудита кодовой базы

| Фича | Статус | Где реализовано |
|---|---|---|
| CLI: run, replay, check, export, ui, version | ✅ Done | `internal/cli/app.go` |
| Парсинг runtime trace через `golang.org/x/exp/trace` (прямой binary reader, без subprocess) | ✅ Done | `internal/tracebridge/xtrace.go` |
| Стриминговый live-парсинг: `TailReader` + `StreamBinaryTrace` + `EngineWriter` (A-1) | ✅ Done | `internal/tracebridge/stream.go`, `internal/analysis/engine.go` |
| Анализ: state machine, timeline, goroutine graph | ✅ Done | `internal/analysis/` |
| Deadlock-hints (циклы в графе ресурсов) | ✅ Done | `internal/analysis/graph.go` |
| Leak-detection (goroutines в WAITING/BLOCKED > threshold) | ✅ Done | `internal/analysis/leak.go` |
| Capture diff (сравнение двух captures) | ✅ Done | `internal/analysis/diff.go`, `/api/v1/compare` |
| Contention-анализ (peak waiters, avg wait) | ✅ Done | `internal/analysis/contention.go` |
| REST API с SSE для live-обновлений | ✅ Done | `internal/api/http.go` |
| React UI: timeline, inspector, compare, metrics chart, groups | ✅ Done | `web/src/` |
| Chrome Trace export | ✅ Done | `web/src/` (кнопка Export Trace) |
| CSV/JSON export | ✅ Done | `internal/cli/app.go` (goroscope export) |
| VS Code extension (session panel, open-in-editor) | ✅ Done | `vscode/` |
| Agent: `StartFromEnv()`, `WithRequestID()` | ✅ Done | `agent/agent.go` |
| CI/CD: GitHub Actions + GitLab CI, race, lint, bench | ✅ Done | `.github/workflows/`, `.gitlab-ci.yml` |
| Zero external Go dependencies (stdlib only) | ✅ Done | `go.mod` (только stdlib) |
| ETag / conditional responses | ✅ Done | `internal/api/http.go` |
| Benchmark regression tracking в CI (A-3) | ✅ Done | `internal/ci/bench_regression.go` + `ci.yml` — benchstat diff + PR comment при регрессии >10% |
| Frontend smoke tests (E-4) | ✅ Done | `web/tests/smoke.test.tsx` — 2 теста, html2canvas замокан, CI запускает `npm run test` |
| Structured logging — нет fmt.Printf/log.Print в production коде (F-3) | ✅ Done | Аудит: `grep` по `internal/`, `cmd/`, `agent/` возвращает 0 hits |
| Goroutine groups view (C-1) | ✅ Done | `internal/analysis/group.go`, `GET /api/v1/goroutines/groups`, `web/src/groups/` |
| Smart Insights engine (C-3) | ✅ Done | `internal/analysis/insights.go`, `GET /api/v1/smart-insights`, `web/src/insights/` |
| Spawn-tree в Inspector (частичная реализация C-2) | ⚠️ Partial | `web/src/inspector/Inspector.tsx` — показывает parent + children в инспекторе, но нет полноценного tree-view и подсветки в timeline |
| Zoom/pan timeline (частичная реализация C-4) | ⚠️ Partial | `web/src/timeline/TimelineCanvas.tsx` — zoom/pan по скроллу + кнопка «Reset zoom», но нет brush-selection с фильтрацией всех панелей |

---

## Категория A — Масштабируемость и производительность

### ~~F-1. Заменить `go tool trace -d=parsed` на `golang.org/x/exp/trace`~~ — ✅ РЕАЛИЗОВАНО (F-1)

> Полностью реализовано: `internal/tracebridge/xtrace.go` — `BuildCaptureFromRawTrace` теперь использует `golang.org/x/exp/trace.NewReader` + `ReadEvent` без subprocess. `ParseParsedTrace` (text-parser) сохранён для обратной совместимости с тестами.

### ~~A-1. Стриминговый парсинг трейсов~~ — ✅ РЕАЛИЗОВАНО

> Полностью реализовано: `internal/tracebridge/stream.go` — `EngineWriter` interface, `TailReader` (следит за растущим файлом, блокируется на EOF), `StreamBinaryTrace` (парсит события и подаёт их в Engine через `ApplyEvent`/`ApplyStackSnapshot`). `watchLiveTrace` в `app.go` заменён на `streamLiveTrace`: O(1) на батч вместо O(n²). `internal/analysis/engine.go` дополнен методами `AddProcessorSegments`, `SetParentIDs`, `SetLabelOverrides`, `Flush`.

---

### A-2. Масштабирование UI до 100k goroutines (P1)

**Gap:** MVP рассчитан на 10-20k goroutines. При больших объёмах timeline UI деградирует — слишком много DOM-элементов, потеря FPS.

**Потребность гоферов:** Визуализация больших трейсов — одна из ключевых жалоб на `go tool trace`. gotraceui (Dominikh) стал популярным именно потому, что рендерит быстрее стандартного инструмента.

**Задача:** Внедрить виртуализацию (react-window или canvas-based рендеринг) для timeline. Реализовать серверную пагинацию и агрегацию goroutine-групп при >10k goroutines.

**Критерий готовности:** Плавный скролл (60fps) при 100k goroutines. Timeline загружается за <3 секунд.

---

### ~~A-3. Benchmark regression tracking в CI~~ — ✅ РЕАЛИЗОВАНО

> Полностью реализовано: `internal/ci/bench_regression.go` + `ci.yml` запускает benchstat, при регрессии >10% создаёт комментарий на PR с полным отчётом и diff-ом.

---

## Категория B — Интеграция с экосистемой

### B-1. OpenTelemetry trace correlation (P1)

**Gap:** Goroscope работает исключительно с runtime trace. Нет связи между goroutine-events и OTel spans. Это ключевая боль сообщества — нельзя увидеть «этот HTTP запрос медленный, потому что goroutine X ждала mutex Y».

**Потребность гоферов:** OpenTelemetry стал де-факто стандартом в 2025. 52% организаций консолидируют инструменты. Корреляция Go runtime trace ↔ OTel trace — unresolved challenge, упомянутый во множестве источников.

**Задача:** Расширить agent-пакет: при наличии OTel span context в goroutine — записывать trace_id/span_id в labels. В UI — отображать OTel span boundaries поверх goroutine timeline. Экспортировать в OTLP-совместимом формате.

**Критерий готовности:** В timeline goroutine видны OTel span boundaries с trace_id. Клик по span открывает ссылку на Jaeger/Grafana Tempo.

---

### B-2. Flight Recorder интеграция (Go 1.25+) (P1)

**Gap:** Go 1.25 представил `runtime/trace.FlightRecorder` — непрерывное low-overhead трейсирование с кольцевым буфером и snapshot по запросу. Goroscope пока не использует этот механизм.

**Потребность гоферов:** Непрерывный профилинг в production — топ-тренд 2025. Pyroscope, Parca набирают популярность именно из-за always-on подхода.

**Задача:** Добавить режим `goroscope attach` — подключение к работающему процессу через Flight Recorder API. Автоматический snapshot при обнаружении аномалии (например, рост goroutine count).

**Критерий готовности:** `goroscope attach --pid=12345` подключается к процессу и показывает live timeline из Flight Recorder snapshot.

---

### B-3. Pyroscope/pprof continuous profiling overlay (P2)

**Gap:** Goroscope показывает goroutine lifecycle, но не CPU/memory profile. Гоферы жалуются, что pprof считает только CPU-время, упуская I/O. fgprof решает это, но данные разрозненны.

**Потребность гоферов:** Объединённый вид «goroutine timeline + CPU flame graph + memory allocation» — то, что ни один инструмент сейчас не предоставляет.

**Задача:** При `goroscope run` параллельно собирать pprof CPU/heap profiles. Отображать flame graph overlay в segment inspector при клике на timeline segment.

**Критерий готовности:** Клик на segment показывает flame graph CPU profiling за этот временной интервал.

---

### B-4. OTLP Export (P2)

**Gap:** Export доступен в CSV, JSON и Chrome Trace. Нет экспорта в формат, который можно загрузить в Grafana/Jaeger/Datadog.

**Потребность гоферов:** Vendor-neutral форматы (OTLP, Parquet) — тренд 2025-2026 для data portability.

**Задача:** Добавить `goroscope export --format=otlp` — конвертация goroutine timeline segments в OTel spans с parent-child relationships. Поддержать отправку через gRPC/HTTP в collector.

**Критерий готовности:** `goroscope export --format=otlp --endpoint=localhost:4317 capture.gtrace` успешно отправляет данные в OTel Collector.

---

## Категория C — Анализ и UX

### ~~C-1. Агрегированный вид goroutine-групп~~ — ✅ РЕАЛИЗОВАНО (2026-03-19)

> **Реализовано:** `GET /api/v1/goroutines/groups?by=function|package|parent_id|label[&label_key=<key>]` — чистая функция `analysis.GroupGoroutines()` агрегирует по выбранному измерению с per-group state-counts, avg/max/total wait, total CPU time из RUNNING-сегментов. Новая вкладка «Groups» в Inspector-панели: collapsible rows, переключатель group-by, поле label_key, клик на ID-badge переходит в Details.

---

### ~~C-2. Улучшенная визуализация parent-child иерархии~~ — ✅ РЕАЛИЗОВАНО (2026-03-19)

> **Реализовано:** `SpawnTree.tsx` — полностью рекурсивное collapsible дерево (auto-expand первые 2 уровня, child count badge, state-dot на каждом чипе). Секция «Ancestors» показывает цепочку root → … → parent → selected. «Highlight branch» собирает все ancestor + descendant IDs, передаёт в app.tsx → Timeline → TimelineCanvas, где goroutines вне ветки получают dark overlay + globalAlpha 0.28. «Clear highlight» снимает подсветку. Highlight автоматически сбрасывается при смене goroutine.

---

### ~~C-3. Автоматические рекомендации (Smart Insights)~~ — ✅ РЕАЛИЗОВАНО (2026-03-19)

> **Реализовано:** `GET /api/v1/smart-insights` — `analysis.GenerateInsights()` синтезирует deadlock/leak/contention/blocking/goroutine-count в ranked список findings (score 0–100, severity critical/warning/info, actionable recommendations). Компонент `SmartInsights` отображается баннером под header: collapsible карточки с иконкой severity, badge critical/warning, G-id бэджи для перехода в инспектор, кнопка Dismiss.

---

### ~~C-4. Интерактивный фильтр по времени (Time Range Selection)~~ — ✅ РЕАЛИЗОВАНО (2026-03-19)

> **Реализовано:** «⌖ Select range» toggle в legend bar Timeline активирует brush mode в TimelineCanvas. Drag создаёт полупрозрачный cyan brush rect поверх rows canvas. По завершении drag вычисляется множество goroutine IDs, у которых есть сегмент в выбранном диапазоне [startNS, endNS]. Это множество передаётся в app.tsx как `brushFilterIds` → `displayGoroutines` сужается. MetricsChart получает `highlightRange` и рисует matching rect. «✕ Clear range» снимает фильтр. Zoom/pan работают независимо и сохраняются.

---

### C-5. Документация для пользователей (P1)

**Gap:** README покрывает установку и базовое использование. Нет guide по интерпретации результатов, best practices, и troubleshooting.

**Потребность гоферов:** Плохая документация — одна из главных претензий к `go tool trace`. Сообщество буквально reverse-engineer'ит поведение инструмента.

**Задача:** Написать user guide: «Understanding goroutine states», «Interpreting deadlock hints», «Using goroscope in CI», «Agent instrumentation guide», «Comparing captures for regression detection».

**Критерий готовности:** Документация на сайте или в docs/ с примерами и скриншотами для каждого use case.

---

## Категория D — Production readiness

### D-1. Аутентификация и TLS для remote-доступа (P2)

**Gap:** API сервер слушает на localhost без аутентификации. SEC-1 из CLAUDE.md требует TLS. Для team-использования нужен remote access.

**Потребность гоферов:** Sharing debug sessions с коллегами — частый запрос в команде.

**Задача:** Добавить `--tls-cert`, `--tls-key` флаги. Опциональная bearer-token аутентификация. При remote mode — принудительный TLS.

**Критерий готовности:** `goroscope run --listen=0.0.0.0:7070 --tls-cert=cert.pem --tls-key=key.pem --token=secret` работает с TLS и аутентификацией.

---

### D-2. Персистентность captures (P2)

**Gap:** Captures живут только в памяти текущей сессии. При перезапуске goroscope — всё теряется.

**Потребность гоферов:** Возможность вернуться к старому capture для сравнения или расследования.

**Задача:** Автоматически сохранять captures в `~/.goroscope/captures/` в формате `.gtrace`. Индекс с метаданными (target, timestamp, duration, goroutine count). UI: history panel с поиском и загрузкой.

**Критерий готовности:** После `goroscope run` capture автоматически сохраняется. `goroscope history` показывает список. Любой capture можно открыть повторно.

---

### D-3. Graceful degradation при больших трейсах (P2)

**Gap:** Нет стратегии для ситуаций, когда трейс слишком большой для текущих ресурсов.

**Потребность гоферов:** Gophers работают с production traces, которые могут быть гигантскими. Инструмент не должен падать.

**Задача:** Реализовать memory budget с sampling. При превышении лимита — автоматически переключаться на sampled view с предупреждением. Приоритизировать goroutines с аномалиями (long wait, deadlock candidates).

**Критерий готовности:** При трейсе 2GB goroscope запускается с warning «sampled view: showing 15k of 250k goroutines (prioritized by anomaly score)» и работает в пределах 1GB RAM.

---

## Категория E — Developer Experience

### E-1. `go test -trace` интеграция (P1)

**Gap:** Goroscope может replay `go test -trace=out.trace`, но нет удобного one-liner и нет связи с тестами.

**Потребность гоферов:** Тестирование конкурентности — типичный use case. Гоферы хотят видеть goroutine поведение конкретного теста.

**Задача:** Добавить `goroscope test ./pkg/...` — обёртка над `go test -trace`, которая автоматически запускает UI с результатом. Фильтрация по test function name.

**Критерий готовности:** `goroscope test -run TestWorkerPool ./pkg/worker` запускает тест, собирает трейс, открывает UI с goroutines отфильтрованными по этому тесту.

---

### E-2. VS Code extension: inline goroutine annotations (P2)

**Gap:** VS Code extension может открыть файл по stack frame. Но нет inline-аннотаций «здесь goroutine X заблокировалась на 200ms».

**Потребность гоферов:** IDE-интеграция для отладки — стандарт в 2025. VS Code занимает 37-43% рынка среди гоферов.

**Задача:** Расширить VS Code extension: при активной сессии показывать CodeLens/inline hints на строках, где goroutines меняли состояние. Цветовая индикация: зелёный (running), жёлтый (waiting), красный (blocked).

**Критерий готовности:** Во время `goroscope run` в VS Code видны inline hints на строках с goroutine activity.

---

### ~~E-3. Homebrew / go install дистрибуция~~ — ✅ РЕАЛИЗОВАНО (2026-03-19)

> **Реализовано:** `.goreleaser.yaml` (v2) — builds for linux/darwin/windows × amd64/arm64, pre-hook `make web`, archives bundle `web/dist/`, sha256 checksums. Homebrew formula auto-published to `Khachatur86/homebrew-goroscope` tap via `HOMEBREW_TAP_TOKEN`. `.github/workflows/release.yml` переписан на `goreleaser/goreleaser-action@v6`. README расширен: `brew install`, `go install`, и manual archive sections.

---

### ~~E-4. Frontend smoke tests~~ — ✅ РЕАЛИЗОВАНО

> `web/tests/smoke.test.tsx` содержит 2 теста с корректным моком `html2canvas`. CI запускает `npm run test` в Vitest. Конфликт ESM разрешён через `vi.mock("html2canvas", ...)`.

---

## Категория F — Качество кода

### F-1. Перейти на `golang.org/x/exp/trace` reader (P1)

**Gap:** Парсинг через subprocess `go tool trace -d=parsed` — хрупкий, зависит от конкретного формата вывода, не стримится.

**Потребность гоферов:** Экспериментальный `golang.org/x/exp/trace` API доступен с Go 1.22+. Это программный доступ к трейсам без fork процесса.

**Задача:** Заменить `go tool trace -d=parsed` на прямой импорт `golang.org/x/exp/trace`. Это первая зависимость — но из official x/ modules, что соответствует MD-1 (prefer stdlib, introduce deps with clear payoff).

**Критерий готовности:** `go.mod` содержит `golang.org/x/exp/trace`. Все существующие тесты проходят. Бенчмарки показывают ≥ паритет по скорости.

---

### F-2. Fuzz testing для trace parser (P2)

**Gap:** SEC-4 из CLAUDE.md рекомендует fuzz tests для untrusted inputs. Trace файлы — untrusted input.

**Потребность гоферов:** Robustness при работе с повреждёнными или необычными trace файлами.

**Задача:** Добавить `func FuzzBuildCaptureFromRawTrace(f *testing.F)` с corpus из реальных трейсов + мутированных вариантов.

**Критерий готовности:** Fuzz test запускается локально. Найденные краши исправлены.

---

### ~~F-3. Structured logging audit~~ — ✅ РЕАЛИЗОВАНО

> Аудит `internal/`, `cmd/`, `agent/` показал 0 вхождений `fmt.Print*` / `log.Print*` вне тестов. Production код не использует неструктурированные логи.

---

## Сводная таблица приоритетов

> ✅ = реализовано, ⚠️ = частично реализовано, открытые задачи — без иконки

| ID | Задача | Приоритет | Категория | Effort | Статус |
|----|--------|-----------|-----------|--------|--------|
| A-1 | Стриминговый парсинг трейсов | P0 | Масштабируемость | L | Открыта |
| C-1 | Агрегированный вид goroutine-групп | P0 | UX | M | ✅ Done |
| B-1 | OpenTelemetry trace correlation | P1 | Интеграция | L | Открыта |
| E-1 | `go test -trace` интеграция | P1 | DevEx | S | ✅ Done |
| A-2 | Масштабирование UI до 100k goroutines | P1 | Масштабируемость | L | Открыта |
| A-3 | Benchmark regression tracking в CI | P1 | Масштабируемость | S | ✅ Done |
| B-2 | Flight Recorder интеграция (Go 1.25+) | P1 | Интеграция | L | Открыта |
| C-2 | Визуализация parent-child иерархии | P1 | UX | M | ✅ Done |
| C-3 | Smart Insights (автоматические рекомендации) | P1 | UX | M | ✅ Done |
| C-4 | Time Range Selection | P1 | UX | M | ✅ Done |
| C-5 | Документация для пользователей | P1 | UX | M | Открыта |
| E-3 | Homebrew / go install дистрибуция | P1 | DevEx | S | ✅ Done |
| E-4 | Frontend smoke tests | P1 | DevEx | S | ✅ Done |
| F-1 | Перейти на x/exp/trace reader | P1 | Код | M | Открыта |
| F-3 | Structured logging audit | P1 | Код | S | ✅ Done |
| B-3 | Pyroscope/pprof overlay | P2 | Интеграция | L | Открыта |
| B-4 | OTLP Export | P2 | Интеграция | M | Открыта |
| D-1 | TLS + аутентификация | P2 | Production | M | Открыта |
| D-2 | Персистентность captures | P2 | Production | M | Открыта |
| D-3 | Graceful degradation (sampling) | P2 | Production | L | Открыта |
| E-2 | VS Code inline annotations | P2 | DevEx | L | Открыта |
| F-2 | Fuzz testing для trace parser | P2 | Код | S | Открыта |

> **Effort:** S = 1-3 дня, M = 1-2 недели, L = 2-4 недели

---

## Рекомендуемый порядок реализации

**Sprint 1 (Quick wins — A-3, E-4, F-3 уже сделаны):**
E-3 → E-1 → C-2 (доработка до полного) → C-4 (brush-selection)

**Sprint 2 (Core UX improvements):**
C-1 → C-4 → C-2 → C-5

**Sprint 3 (Scalability foundation — зависимость: F-1 разблокирует A-1 и E-1):**
F-1 → A-1 → E-1 → A-2

**Sprint 4 (Ecosystem integration):**
B-1 → B-2 → B-4

**Sprint 5 (Production & polish):**
D-2 → D-1 → C-3 → D-3

**Sprint 6 (Advanced):**
B-3 → E-2 → F-2
