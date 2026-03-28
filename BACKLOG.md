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

### ~~A-2. Масштабирование UI до 100k goroutines~~ — ✅ РЕАЛИЗОВАНО

> **Реализовано:** Устранены все O(n²) узкие места: `segmentsByGoroutine Map<gid, segments>` в TimelineCanvas (O(1) lookup вместо `.filter()` на каждой строке), `goroutineIdSet` в Timeline для O(1) фильтрации, `segmentMap` в Timeline для O(goroutines) scrubSnapshot. Fix stack overflow: `Math.min/max(...spread)` заменены на loop-based в TimelineCanvas, Timeline.tsx, LifetimeBar. Lazy-загрузка сегментов: Timeline грузит батчами по 150 goroutines по мере скролла через `onVisibleRangeChange` callback (debounce 120ms), с буфером ±150 строк. Backend: `GET /api/v1/timeline?goroutine_ids=1,2,3` поддерживает O(1) Map lookup для быстрой фильтрации. С D-3 sampling (15k goroutine cap) и lazy-loading timeline отрисовывается за <3с при 100k goroutines.

---

### ~~A-3. Benchmark regression tracking в CI~~ — ✅ РЕАЛИЗОВАНО

> Полностью реализовано: `internal/ci/bench_regression.go` + `ci.yml` запускает benchstat, при регрессии >10% создаёт комментарий на PR с полным отчётом и diff-ом.

---

## Категория B — Интеграция с экосистемой

### ~~B-1. OpenTelemetry trace correlation~~ — ✅ РЕАЛИЗОВАНО

> **Реализовано:** `agent/otel.go` — `WithOTelSpan(ctx, traceID, spanID)` прокидывает OTel контекст через pprof labels (`otel.trace_id`, `otel.span_id`) и sidecar-файл без внешних зависимостей (caller передаёт строки). `OTelSpanFromLabels()` — helper для извлечения. Timeline canvas: бирюзовый бейдж "OT" на строках goroutines с активным `otel.trace_id`. Inspector: выделенная секция с trace_id/span_id, кнопками копирования и clickable-ссылками на Jaeger (`{base}/trace/{traceId}`) и Grafana Tempo (explore URL с TraceQL-запросом). URLs сохраняются в localStorage. `otel.*` labels скрыты из общего Labels-блока. 4 таблично-параллельных теста.

---

### ~~B-2. Flight Recorder интеграция (Go 1.25+)~~ — ✅ РЕАЛИЗОВАНО

**Gap:** Go 1.25 представил `runtime/trace.FlightRecorder` — непрерывное low-overhead трейсирование с кольцевым буфером и snapshot по запросу. Goroscope пока не использует этот механизм.

**Потребность гоферов:** Непрерывный профилинг в production — топ-тренд 2025. Pyroscope, Parca набирают популярность именно из-за always-on подхода.

**Задача:** Добавить режим `goroscope attach` — подключение к работающему процессу через Flight Recorder API. Автоматический snapshot при обнаружении аномалии (например, рост goroutine count).

**Критерий готовности:** `goroscope attach --pid=12345` подключается к процессу и показывает live timeline из Flight Recorder snapshot.

---

### ~~B-3. Pyroscope/pprof continuous profiling overlay~~ — ✅ РЕАЛИЗОВАНО

> **Реализовано:** `Engine.GetStacksInRange(startNS, endNS)` — новый метод в analysis engine, возвращает все `StackSnapshot` в заданном временном окне (cross-goroutine). `GET /api/v1/pprof/stacks?start_ns=X&end_ns=Y` — новый API endpoint. `fetchPprofStacks(startNs, endNs)` в `client.ts`. `FlameGraph` расширен: принимает `externalSamples?: StackSnapshot[]` — пропускает fetch, рендерит переданные samples напрямую. `Inspector.tsx` — коллапсируемая секция «CPU profile @ segment» появляется при выборе сегмента, показывает cross-goroutine flame graph за `[start_ns, end_ns]` окно сегмента с бирюзовым hint-лейблом.

---

### ~~B-4. OTLP Export~~ — ✅ РЕАЛИЗОВАНО

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

### ~~C-5. Документация для пользователей~~ — ✅ РЕАЛИЗОВАНО

> **Реализовано:** `docs/user-guide/` — 5 guide-файлов: `goroutine-states.md` (таблица состояний, anomaly score), `interpreting-results.md` (Smart Insights, deadlock hints, leak detection, contention), `ci-integration.md` (GitHub Actions / GitLab CI, `goroscope check`, benchmark regression), `agent-guide.md` (StartFromEnv, Flight Recorder, request correlation, goroutine labels, security), `compare-captures.md` (capture diff, regression gate, API reference). `README.md` — index с quick start и key concepts.

---

## Категория D — Production readiness

### ~~D-1. Аутентификация и TLS для remote-доступа~~ — ✅ РЕАЛИЗОВАНО

**Gap:** API сервер слушает на localhost без аутентификации. SEC-1 из CLAUDE.md требует TLS. Для team-использования нужен remote access.

**Потребность гоферов:** Sharing debug sessions с коллегами — частый запрос в команде.

**Задача:** Добавить `--tls-cert`, `--tls-key` флаги. Опциональная bearer-token аутентификация. При remote mode — принудительный TLS.

**Критерий готовности:** `goroscope run --listen=0.0.0.0:7070 --tls-cert=cert.pem --tls-key=key.pem --token=secret` работает с TLS и аутентификацией.

---

### ~~D-2. Персистентность captures~~ — ✅ РЕАЛИЗОВАНО

**Gap:** Captures живут только в памяти текущей сессии. При перезапуске goroscope — всё теряется.

**Потребность гоферов:** Возможность вернуться к старому capture для сравнения или расследования.

**Задача:** Автоматически сохранять captures в `~/.goroscope/captures/` в формате `.gtrace`. Индекс с метаданными (target, timestamp, duration, goroutine count). UI: history panel с поиском и загрузкой.

**Критерий готовности:** После `goroscope run` capture автоматически сохраняется. `goroscope history` показывает список. Любой capture можно открыть повторно.

---

### ~~D-3. Graceful degradation при больших трейсах~~ — ✅ РЕАЛИЗОВАНО

**Gap:** Нет стратегии для ситуаций, когда трейс слишком большой для текущих ресурсов.

**Потребность гоферов:** Gophers работают с production traces, которые могут быть гигантскими. Инструмент не должен падать.

**Задача:** Реализовать memory budget с sampling. При превышении лимита — автоматически переключаться на sampled view с предупреждением. Приоритизировать goroutines с аномалиями (long wait, deadlock candidates).

**Критерий готовности:** При трейсе 2GB goroscope запускается с warning «sampled view: showing 15k of 250k goroutines (prioritized by anomaly score)» и работает в пределах 1GB RAM.

---

## Категория E — Developer Experience

### ~~E-1. `go test -trace` интеграция~~ — ✅ РЕАЛИЗОВАНО (E-1)

> **Реализовано:** `goroscope test` уже запускал `go test -trace=<tmpfile>` и поднимал UI. Добавлена фильтрация по test function name: `extractRunFilter(args)` парсит `-run=<value>` и `-run <value>` из аргументов go test. Новый флаг `--filter` для явного переопределения. При открытии браузера к URL добавляется `?search=<filter>` — UI открывается с уже заполненным полем поиска. Примеры: `goroscope test -run TestWorkerPool ./pkg/worker -open-browser` → браузер открывается с goroutines, отфильтрованными по "TestWorkerPool". Юнит-тест `TestExtractRunFilter` (8 cases) + интеграционный `TestRun_Test_FilterFlag`.

---

### ~~E-2. VS Code extension: inline goroutine annotations~~ — ✅ РЕАЛИЗОВАНО

> **Реализовано:** `vscode/src/annotation.ts` — `AnnotationController` класс: поллит `GET /api/v1/goroutines?limit=500` каждые 3 секунды, строит `TextEditorDecorationType` per-state (BLOCKED=красный, WAITING=янтарный, SYSCALL=синий, RUNNING=зелёный), применяет inline `after`-hints с goroutine ID и wait time (`← G42 1.2s`) на строках из `last_stack.frames[0]`. Hover tooltip с полными деталями. Команда `goroscope.toggleAnnotations` включает/выключает аннотации. Конфиг `goroscope.inlineAnnotations` (default: true). `AnnotationController` стартует при `activate()`, учитывает изменение `goroscope.addr` через `onDidChangeConfiguration`.

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

### ~~F-2. Fuzz testing для trace parser~~ — ✅ РЕАЛИЗОВАНО

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
| A-1 | Стриминговый парсинг трейсов | P0 | Масштабируемость | L | ✅ Done |
| C-1 | Агрегированный вид goroutine-групп | P0 | UX | M | ✅ Done |
| B-1 | OpenTelemetry trace correlation | P1 | Интеграция | L | ✅ Done |
| E-1 | `go test -trace` интеграция | P1 | DevEx | S | ✅ Done |
| A-2 | Масштабирование UI до 100k goroutines | P1 | Масштабируемость | L | ✅ Done |
| A-3 | Benchmark regression tracking в CI | P1 | Масштабируемость | S | ✅ Done |
| B-2 | Flight Recorder интеграция (Go 1.25+) | P1 | Интеграция | L | ✅ Done |
| C-2 | Визуализация parent-child иерархии | P1 | UX | M | ✅ Done |
| C-3 | Smart Insights (автоматические рекомендации) | P1 | UX | M | ✅ Done |
| C-4 | Time Range Selection | P1 | UX | M | ✅ Done |
| C-5 | Документация для пользователей | P1 | UX | M | ✅ Done |
| E-3 | Homebrew / go install дистрибуция | P1 | DevEx | S | ✅ Done |
| E-4 | Frontend smoke tests | P1 | DevEx | S | ✅ Done |
| F-1 | Перейти на x/exp/trace reader | P1 | Код | M | ✅ Done |
| F-3 | Structured logging audit | P1 | Код | S | ✅ Done |
| B-3 | Pyroscope/pprof overlay | P2 | Интеграция | L | ✅ Done |
| B-4 | OTLP Export | P2 | Интеграция | M | ✅ Done |
| D-1 | TLS + аутентификация | P2 | Production | M | ✅ Done |
| D-2 | Персистентность captures | P2 | Production | M | ✅ Done |
| D-3 | Graceful degradation (sampling) | P2 | Production | L | ✅ Done |
| E-2 | VS Code inline annotations | P2 | DevEx | L | ✅ Done |
| F-2 | Fuzz testing для trace parser | P2 | Код | S | ✅ Done |
| G-1 | Full call-stack search | P1 | Анализ | S | ✅ Done |
| G-5 | HTTP request correlation view | P1 | Интеграция | L | ✅ Done |
| U-1 | Drag-to-resize panels | P2 | UI | S | ✅ Done |
| U-2 | Playback mode | P2 | UI | M | ✅ Done |
| U-3 | Goroutine watchlist / pinning | P2 | UI | S | ✅ Done |
| G-2 | Resource contention heatmap | P2 | Анализ | M | ✅ Done |
| G-3 | Goroutine birth/death markers | P2 | UI | S | ✅ Done |
| G-4 | `goroscope watch` — anomaly alerts | P2 | CLI | M | ✅ Done |
| U-4 | Timeline bookmarks | P3 | UI | S | ✅ Done |
| U-5 | Dark/light theme + accent color | P3 | UI | S | ✅ Done |
| H-1 | Stack frame search в API | P1 | Backend | S | ✅ Done |
| H-4 | Request correlation engine + API | P1 | Backend | L | ✅ Done |
| H-6 | SSE delta streaming | P1 | Backend | M | ✅ Done |
| H-2 | Goroutine lifecycle timestamps в API | P2 | Backend | S | ✅ Done |
| H-3 | Contention heatmap endpoint | P2 | Backend | M | ✅ РЕАЛИЗОВАНО (H-3) |
| H-5 | Prometheus `/metrics` endpoint | P2 | Backend | S | ✅ Done |
| H-7 | Multi-process monitoring | P2 | Backend | L | ✅ РЕАЛИЗОВАНО (H-7) |
| I-1 | OpenAPI spec + generated TS client | P2 | Infrastructure | S | ✅ РЕАЛИЗОВАНО (I-1) |
| I-4 | Engine incremental recompute | P1 | Infrastructure | M | ✅ Done |
| I-5 | API integration test suite | P1 | Infrastructure | M | ✅ Done |
| I-2 | Docker image + Compose пример | P2 | Infrastructure | S | ✅ РЕАЛИЗОВАНО (I-2) |
| I-6 | CSP + CORS headers | P2 | Infrastructure | S | ✅ Done |
| I-7 | `goroscope diff` CLI command | P2 | Infrastructure | S | ✅ Done |
| I-9 | Stack pattern diff across captures | P2 | Infrastructure | M | ✅ РЕАЛИЗОВАНО (I-9) |
| I-3 | Shell autocomplete | P3 | Infrastructure | S | ✅ РЕАЛИЗОВАНО (I-3) |
| I-8 | `goroscope annotate` command | P3 | Infrastructure | S | ✅ РЕАЛИЗОВАНО (I-8) |
| I-10 | WASM offline mode | P3 | Infrastructure | L | ✅ РЕАЛИЗОВАНО (I-10) |

> **Effort:** S = 1-3 дня, M = 1-2 недели, L = 2-4 недели

---

## Категория U — UI / UX качество

### U-1. Drag-to-resize panels (P2)

**Gap:** Три панели (goroutine list / timeline / inspector) имеют фиксированные CSS-пропорции. Пользователь не может адаптировать layout под свой экран или задачу.

**Задача:** Добавить draggable divider между панелями. Сохранять размеры в localStorage. При очень маленьком экране — collapse до иконки.

**Критерий готовности:** Пользователь перетаскивает разделитель, панели растягиваются/сужаются, размер восстанавливается после перезагрузки страницы.

---

### U-2. Playback mode — анимация scrubber'а (P2)

**Gap:** Scrubber времени есть, но его нужно двигать вручную. Нет возможности «воспроизвести» события как видео.

**Задача:** Добавить панель управления воспроизведением: ▶ Play / ⏸ Pause / ⏩ 2× / ⏩ 4× / ⏮ Reset. Play автоматически двигает scrubTimeNS вперёд с заданной скоростью (requestAnimationFrame). Скорость — коэффициент к real-time.

**Критерий готовности:** Нажатие Play анимирует scrubber от начала до конца записи, goroutine list и состояния обновляются в реальном времени.

---

### U-3. Goroutine watchlist / pinning (P2)

**Gap:** При работе с сотнями goroutines отслеживаемые goroutines теряются при смене фильтров.

**Задача:** Кнопка «⭐» (pin) у каждой goroutine. Закреплённые goroutines всегда отображаются вверху списка вне зависимости от активных фильтров. Возможность добавить короткую заметку (до 80 символов). Хранить в localStorage по goroutine ID.

**Критерий готовности:** Goroutine закрепляется, остаётся видна при любых фильтрах, заметка отображается в goroutine row и inspector.

---

### ~~U-4. Timeline bookmarks~~ — ✅ РЕАЛИЗОВАНО (U-4)

> **Реализовано:** `web/src/timeline/bookmarks.ts` — `Bookmark` тип, `loadBookmarks` (localStorage + `?bm=` URL-параметр), `saveBookmarks` (пишет в localStorage и обновляет URL через `history.replaceState`). В `TimelineCanvas.tsx`: двойной клик на ось вызывает `onAddBookmarkRequest(timeNS)` вместо сброса скраббера; фиолетовые пунктирные вертикали рендерятся в `renderAxis` и `renderRows`; hover на линию ±6px показывает `bookmark-tooltip` с именем, временем и кнопкой Delete. В `Timeline.tsx`: state `bookmarks` (init из `loadBookmarks`), диалог `bookmark-dialog-overlay` (поле имени + Add/Cancel, клавиши Enter/Escape), `deleteBookmark` callback. CSS: `.bookmark-dialog-overlay`, `.bookmark-dialog`, `.bookmark-tooltip`, `.bookmark-tooltip-delete`.

---

### U-5. Dark / light theme + accent color (P3)

**Gap:** UI только тёмный, нет системы тем.

**Задача:** Переписать все цвета через CSS-переменные (уже частично есть). Добавить switcher dark / light / system. 5-6 preset акцентных цветов (teal, blue, amber, rose, purple, green). Хранить в localStorage. Учесть prefers-color-scheme.

**Критерий готовности:** Смена темы без перезагрузки. Все компоненты (timeline canvas, inspector, pills) корректно адаптируются к светлой теме.

---

## Категория G — Новый функционал

### G-1. Full call-stack search (P1)

**Gap:** Текущий поиск матчит только верхний фрейм (`labels.function`) или поле `reason`. Нет поиска по произвольному фрейму в call stack.

**Потребность:** При production incident нужно найти «все goroutines с `database/sql.(*DB).QueryContext` в стеке» — это невозможно без полного stack search.

**Задача:** Расширить поле поиска: если запрос начинается с `stack:` — ищет вхождение в любом фрейме сохранённого стека goroutine. Backend: `GET /api/v1/goroutines?stack_frame=database/sql`. Engine сканирует `last_stack.frames` goroutines. Подсветка совпавших фреймов в inspector.

**Критерий готовности:** `stack:net/http` находит все goroutines с http-фреймами в стеке. Работает с 10k+ goroutines за < 200ms.

---

### ~~G-2. Resource contention heatmap view~~ — ✅ РЕАЛИЗОВАНО (G-2)

> **Реализовано:** `ContentionHeatmap` компонент уже содержал Canvas-based 2D heatmap (X=время bins, Y=resource ID, цвет=concurrent waiters). Добавлен drill-down: клик на ячейку теперь вычисляет `bucketMidNS` (середина временного bucket) и вызывает `onSelectResource(resourceId, bucketMidNS)`. В `app.tsx`: callback устанавливает `scrubTimeNS = bucketMidNS` И `search = resourceId`, что одновременно скраббирует timeline к нужному моменту и фильтрует goroutine list к ожидателям этого ресурса. Tooltip обновлён: «Click cell to scrub + filter».

---

### G-3. Goroutine birth/death markers на timeline (P2)

**Gap:** Timeline показывает состояния goroutines, но не показывает моменты их создания и завершения явно.

**Задача:** Парсить из engine первый и последний сегмент каждой goroutine как born/died timestamps. На timeline canvas рисовать маркеры: ▲ (born, зелёный) и ▼ (died, серый) на горизонтальной оси событий над lanes. Toggle «Show lifecycle markers» в legend bar. Hover на маркер — тултип с ID и временем.

**Критерий готовности:** Маркеры видны при зуме. При 10k goroutines рендеринг не деградирует (batching/culling по viewport).

---

### G-4. `goroscope watch` — live anomaly alerting (P2)

**Gap:** Goroscope — интерактивный инструмент. Нет режима headless-мониторинга для CI/production без UI.

**Задача:** Новая CLI-команда `goroscope watch [flags] <target>`. Подключается к live trace (через Flight Recorder или debug endpoint). При срабатывании условия — эмитит structured JSON alert в stdout (pipe-friendly) и/или POST на webhook.

Флаги: `--alert-goroutines=500` (count threshold), `--alert-block-ms=5000` (goroutine blocked longer than), `--alert-deadlock` (любой deadlock hint), `--webhook=https://hooks.slack.com/...`, `--once` (exit after first alert).

**Критерий готовности:** `goroscope watch --alert-goroutines=100 --webhook=... http://app:6060` в background-процессе, при превышении порога — Slack-нотификация с goroutine count и top-5 blocked goroutines.

---

### ~~G-5. HTTP request correlation view~~ — ✅ РЕАЛИЗОВАНО (G-5 + H-4)

> **Реализовано:** Backend — `internal/analysis/requests.go`: `RequestGroup` struct + `Engine.GroupByRequest()`. Pass 1: группировка по labels `http.request_id`/`request_id`/`trace_id`. Pass 2 (fallback): BFS по дереву потомков от goroutines с `net/http.(*conn).serve` в стеке. Эндпоинты: `GET /api/v1/requests` (список групп), `GET /api/v1/requests/{id}/goroutines` (goroutines запроса). Frontend — `web/src/requests/RequestsView.tsx`: поиск, список групп с METHOD badge, URL, duration, goroutine count, state breakdown bar; клик → `setHighlightedIds`. Новая вкладка «Requests» в analysis panel.

---

## Категория H — Backend improvements

### H-1. Stack frame search в API (P1)

**Gap:** `GET /api/v1/goroutines` матчит только верхний фрейм / поле reason. Нет поиска по произвольному фрейму в call stack.

**Задача:** Добавить query-параметр `?stack_frame=<substring>` в `GET /api/v1/goroutines`. Engine сканирует `last_stack.frames` каждой goroutine. Опционально: обратный индекс `frame_string → []goroutine_id` в Engine для O(1) lookup при повторных запросах.

**Критерий готовности:** `GET /api/v1/goroutines?stack_frame=net/http` возвращает только goroutines с совпадением в любом фрейме. Работает за <200ms при 10k goroutines.

---

### H-2. Goroutine lifecycle timestamps в API (P2)

**Gap:** Engine знает первый/последний сегмент каждой goroutine, но `GET /api/v1/goroutines` не возвращает `born_ns` / `died_ns`. Без этого нельзя отрисовать birth/death markers и посчитать latency HTTP-запросов.

**Задача:** При построении segments добавлять `born_ns` (start_ns первого сегмента) и `died_ns` (end_ns последнего сегмента, если goroutine завершена) в модель goroutine. Включить в JSON-ответ. Новое поле `is_alive bool` — false если died_ns заполнен.

**Критерий готовности:** Каждая goroutine в `/api/v1/goroutines` содержит `born_ns`. Завершённые goroutines содержат `died_ns` и `"is_alive": false`.

---

### ~~H-3. Contention heatmap endpoint~~ — ✅ РЕАЛИЗОВАНО (H-3)

**Gap:** Contention-данные накапливаются в Engine, но только как статичные агрегаты (peak_waiters, avg_wait). Нет временно́го измерения — невозможно построить heatmap.

**Задача:** Engine при каждом обновлении записывает snaphot contention (timestamp + per-resource waiters count) в кольцевой буфер. Новый endpoint `GET /api/v1/contention/heatmap?resolution_ms=100&limit_resources=50` возвращает матрицу: `{ bins: [...timestamps], resources: [{id, label, counts: [...]}] }`. Агрегация: max waiters в каждом bin.

**Критерий готовности:** Endpoint возвращает данные за всё время сессии с заданным разрешением. 1000 ресурсов × 1000 временных bins за <100ms.

---

### ~~H-4. Request correlation engine + API~~ — ✅ РЕАЛИЗОВАНО (см. G-5)

**Gap:** Нет понятия «HTTP-запрос» в модели данных. Goroutines, обслуживающие один запрос, не связаны между собой в API.

**Задача:** Новая функция `analysis.GroupByRequest()` — группирует goroutines по pprof-labels (`http.request_id`, `request_id`, `trace_id`) или по parent-chain от goroutine с `net/http.(*conn).serve` в стеке. Новые endpoints:
- `GET /api/v1/requests` — список request-групп: `{request_id, url, method, start_ns, end_ns, goroutine_count, state_breakdown}`
- `GET /api/v1/requests/{id}/goroutines` — goroutines конкретного запроса

**Критерий готовности:** При наличии `http.request_id` labels — запросы корректно группируются. Fallback на `net/http`-стек-матчинг. 1000+ concurrent requests обрабатываются за <500ms.

---

### H-5. Prometheus `/metrics` endpoint (P2)

**Gap:** Нет стандартного способа интегрировать goroscope в существующий Grafana/Prometheus стек. Нужно заходить в UI и читать глазами.

**Задача:** Новый handler `GET /metrics` в формате Prometheus text exposition (нулевые зависимости — plain text). Метрики:
```
goroscope_goroutines_total{state="BLOCKED"} 42
goroscope_goroutines_total{state="RUNNING"} 8
goroscope_deadlock_hints_total 1
goroscope_leak_candidates_total 3
goroscope_memory_budget_used_bytes 134217728
goroscope_session_duration_seconds 3600
```

**Критерий готовности:** `curl http://localhost:7070/metrics` возвращает корректный Prometheus text format. Prometheus scrape_config работает без доп. настроек.

---

### H-6. SSE delta streaming (P1)

**Gap:** `GET /api/v1/stream` отправляет полный snapshot goroutines на каждый тик. При 10k goroutines — ~10MB/s трафика. Браузер парсит весь JSON заново.

**Задача:** Перейти на diff-формат. Engine версионирует состояние (monotonic revision counter). SSE-событие содержит: `revision`, `added: []Goroutine`, `updated: []Goroutine`, `removed: []goroutine_id`. Клиент присылает текущий revision через `Last-Event-ID`. При первом подключении (revision=0) — full snapshot. Engine хранит diff за последние N ревизий (ring buffer, N=10).

**Критерий готовности:** При 0 изменений между тиками SSE-событие содержит пустые `added/updated/removed`. Трафик при стабильном состоянии → ~0. Фронт обновляет только изменившиеся строки.

---

### ~~H-7. Multi-process monitoring~~ — ✅ РЕАЛИЗОВАНО (H-7)

> **Реализовано:** Новый пакет `internal/target` — `Registry` управляет несколькими targets, каждый со своим `Engine` + pprof-поллером (goroutine lifetime tied to context, CC-2). API: `GET /api/v1/targets`, `POST /api/v1/targets {addr, label}`, `DELETE /api/v1/targets/{id}`. `Server.WithRegistry(r)` привязывает registry к серверу; хелперы `engineFor(r)` и `sessionsFor(r)` роутят запросы по `?target_id=`. CLI: `goroscope ui --target=http://localhost:6060 --target=label=http://localhost:6061`. `parseTargetSpec` поддерживает формат `label=http://addr`. Юнит-тесты: `TestRegistry_*` (4 кейса) + `TestParseTargetSpec` (5 кейсов).

---

## Категория I — Infrastructure & General

### ~~I-1. OpenAPI spec + TypeScript client~~ — ✅ РЕАЛИЗОВАНО (I-1)

> **Реализовано:** `internal/api/openapi.yaml` (OpenAPI 3.1) покрывает все 28 публичных endpoints: goroutines, timeline, insights, compare, stream, replay, targets, metrics. 20+ JSON-схем (Goroutine, Session, StackSnapshot, CaptureDiff, StackPatternDiffResult, TargetInfo и др.). Spec встроен в бинарь через `//go:embed` и доступен на `GET /api/openapi.yaml`. Swagger UI (CDN) — `GET /api/docs`. Makefile: `make gen-client` запускает `npx openapi-typescript` → `web/src/api/schema.d.ts`.

---

### ~~I-2. Docker image + docker-compose пример~~ — ✅ РЕАЛИЗОВАНО (I-2)

> **Реализовано:** `Dockerfile` — multi-stage build (node:22-alpine → golang:1.24-alpine → scratch), React UI встроен через `go:embed`. `Dockerfile.demo` — sample app (trace_demo) для docker-compose. `docker-compose.yml` — полный стек: goroscope attach + demo app, `docker compose up` → UI на :7070. `.goreleaser.yaml` расширен: dockers (amd64 + arm64 с buildx) + docker_manifests (multi-arch тег). Makefile: `make docker`, `make docker-push`, `make docker-compose-up/down`.

---

### ~~I-3. Shell autocomplete (zsh / bash / fish)~~ — ✅ РЕАЛИЗОВАНО (I-3)

**Gap:** CLI не поддерживает автодополнение. `goroscope <Tab>` ничего не делает.

**Задача:** Добавить `goroscope completion zsh|bash|fish` команду. Генерирует completion script для shell'а. README: инструкция установки (`eval "$(goroscope completion zsh)"`). Покрыть все команды, флаги и их значения (например, `--format=json|csv|otlp`).

**Критерий готовности:** Tab-completion работает для всех команд и флагов в zsh и bash. Fish как бонус.

---

### I-4. Engine incremental recompute (P1)

**Gap:** При каждом SSE-тике Engine полностью пересчитывает insights, contention, groups — даже если изменилось 3 goroutine из 10k. O(n) на каждый тик без необходимости.

**Задача:** Добавить dirty-флаги на уровне Engine: `insightsDirty`, `contentionDirty`, `groupsDirty`. Пересчёт только при `dirty=true`. Установка флагов — только при изменении зависимых данных. Добавить бенчмарк `BenchmarkEngineUpdate` в CI (PERF-1).

**Критерий готовности:** При 0 изменений между тиками — 0 пересчётов. Бенчмарк показывает ≥5× улучшение CPU при стабильном состоянии 10k goroutines.

---

### I-5. API integration test suite (P1)

**Gap:** Есть unit-тесты engine и frontend smoke-тесты. Нет black-box тестов для HTTP API end-to-end.

**Задача:** `internal/api/integration_test.go` — тесты запускают реальный сервер (`httptest.NewServer`), генерируют synthetic trace через engine, вызывают все endpoints и проверяют: HTTP status, JSON schema, ETag, auth. Отдельный тест для SSE stream. Запускается в CI (`go test -run TestIntegration ./internal/api/...`).

**Критерий готовности:** 100% публичных endpoints покрыты хотя бы одним happy-path тестом. Тесты hermetic (нет сетевых вызовов). CI зелёный.

---

### I-6. Content Security Policy + CORS headers (P2)

**Gap:** HTTP-ответы не содержат security headers. SEC-1/SEC-3 из CLAUDE.md. При remote-доступе (D-1) нет контроля CORS origins.

**Задача:** Middleware добавляет заголовки: `Content-Security-Policy`, `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: strict-origin`. Флаг `--cors-origins=https://team.example.com` для whitelist CORS. При `Token` включён — HSTS header. Unit-тест middleware.

**Критерий готовности:** `curl -I http://localhost:7070/` возвращает все security headers. CORS preflight проходит для whitelisted origin, блокируется для остальных.

---

### I-7. `goroscope diff` CLI command (P2)

**Gap:** Сравнение captures доступно только через UI. Нет способа использовать в CI-скриптах или сравнить файлы без браузера.

**Задача:** `goroscope diff baseline.gtrace compare.gtrace` — текстовый вывод: новые/исчезнувшие goroutines, изменения состояний, contention delta. Флаги: `--format=text|json`, `--threshold=10%` (exit code 1 если регрессия выше порога). Переиспользует `analysis.Diff()` который уже есть.

**Критерий готовности:** `goroscope diff a.gtrace b.gtrace --format=json | jq .` работает в shell pipe. CI job использует exit code для regression gate.

---

### ~~I-8. `goroscope annotate` command~~ — ✅ РЕАЛИЗОВАНО (I-8)

**Gap:** .gtrace файлы содержат только goroutine данные. Нет способа добавить контекст: «тут началась деградация», «это после деплоя v2.3».

**Задача:** `goroscope annotate capture.gtrace --at=<timestamp|offset> --note="latency spike"` — добавляет аннотацию в файл. `goroscope annotate capture.gtrace --list` — показывает все аннотации. UI автоматически загружает аннотации как U-4 bookmarks при открытии файла.

**Критерий готовности:** Аннотации сохраняются в .gtrace, не ломают обратную совместимость. UI показывает их как именованные закладки.

---

### ~~I-9. Stack pattern diff across captures~~ — ✅ РЕАЛИЗОВАНО (I-9)

> **Реализовано:** `analysis.StackPatternDiff(baseline, compare Capture) StackPatternDiffResult` — нормализует стеки (func names без line numbers), строит map-based signature set, возвращает `{appeared, disappeared, common_count}`. Нормализация стабильна к сдвигам строк. Результаты отсортированы по count desc. `readCaptureFormFile` — shared helper для multipart-парсинга (DRY с `handleCompare`). Новый endpoint `POST /api/v1/compare/stacks` (file_a=baseline, file_b=compare). 6 unit-тестов: empty, all-new, all-gone, common+diff, line-numbers-ignored, count-aggregation, sorted-by-count.

---

### ~~I-10. WASM offline mode~~ — ✅ РЕАЛИЗОВАНО (I-10)

**Gap:** Goroscope требует запущенного Go-сервера. В air-gapped окружениях или при быстром шаринге трейса — барьер входа.

**Задача:** Скомпилировать analysis engine в WebAssembly (`GOOS=js GOARCH=wasm`). Статическая HTML-страница: drag-and-drop .gtrace → анализ в браузере, без сервера. Подмножество функций: goroutine list, basic insights, timeline. Публиковать как GitHub Pages artifact в CI.

**Критерий готовности:** `index.html` + `engine.wasm` открываются локально (file://) без сервера. Drag .gtrace → goroutine list появляется за <3s для 10k goroutines.

---

## RFC: Developer Productivity Pack — ✅ РЕАЛИЗОВАНО

> Добавлено по инициативе «что бы не хватало разработчикам».

| ID | Фича | Статус | Где реализовано |
|----|------|--------|-----------------|
| RFC-U1 | Flamegraph (`BuildFlamegraph`, `FoldedStacks`) | ✅ Done | `internal/analysis/flamegraph.go`, `GET /api/v1/flamegraph`, `GET /api/v1/flamegraph/folded`, `goroscope export --format=flamegraph\|folded` |
| RFC-U2 | `goroscope doctor` — HTML диагностический отчёт | ✅ Done | `internal/cli/doctor.go` — insights, deadlocks, contention, inline flamegraph |
| RFC-U3 | SARIF output для `goroscope check` | ✅ Done | `DeadlockReport.WriteSARIF()`, `goroscope check --format=sarif` |
| RFC-U4 | Slack Incoming Webhook алерты | ✅ Done | `goroscope watch --slack-url=...` — Block Kit формат |
| RFC-U5 | `goroscope top` — live TUI таблица goroutines | ✅ Done | `internal/cli/top.go` — ANSI, blocked-first, `--n`, `--once` |

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

**Sprint 7 (Backend foundation для новых фич):**
H-1 → H-2 → H-5 → H-6 → H-3 → H-4 → H-7

**Sprint 8 (UI quality + новый функционал):**
G-1 → U-1 → U-3 → G-4 → U-2 → G-3 → G-2 → U-4 → G-5 → U-5

**Sprint 9 (Infrastructure & general quality):**
I-4 → I-5 → I-6 → I-1 → I-7 → I-9 → I-2 → I-3 → I-8 → I-10

**Sprint 10 (UI Upgrade — Design System):**
DS-1 → DS-2 → DS-3 → DS-4 → DS-5 → DS-6

---

## Категория DS — Design System (RFC-001)

> Источник: RFC-001 «Design System Foundation» — итерация 1 из 3 плана UI Upgrade.
> Порядок: строго последовательный (каждый юнит зависит от предыдущего).

### ~~DS-1. Centralized color tokens~~ — ✅ РЕАЛИЗОВАНО (DS-1)

> **Реализовано:** `web/src/theme/tokens.ts` — единый источник цветов: `STATE_COLORS` (6 состояний), `COLOR_UNKNOWN`, `DIFF_COLORS`, семантические `COLOR_ERROR/WARNING/SUCCESS/INFO`. Все 5 файлов (`DependencyGraph.tsx`, `LifetimeBar.tsx`, `RequestsView.tsx`, `TimelineCanvas.tsx`, `Timeline.tsx`) заменили локальные дубли на импорт из `tokens.ts`. `tsc --noEmit` и `vite build` проходят.

---

### ~~DS-2. CSS token expansion~~ — ✅ РЕАЛИЗОВАНО (DS-2)

> **Реализовано:** `:root` расширен с 15 до 45+ токенов: spacing scale `--space-1`…`--space-12` (4px–48px), border radius scale `--radius-sm/base/md/lg/full`, 9 typography tokens `--text-2xs`…`--text-3xl`, `--font-medium/semibold/bold`, `--leading-tight/normal/relaxed`, interactive overlays `--overlay-hover/active/focus` и `--opacity-disabled`. Все `font-size:`, `font-weight:`, `border-radius:` magic-numbers заменены на `var()` через скрипт. `tsc --noEmit` и `vite build` проходят.

---

### ~~DS-3. State colors deduplication~~ — ✅ РЕАЛИЗОВАНО (DS-3)

> **Реализовано:** `tokens.ts` расширен BG/TEXT-палитрой для canvas-компонентов (`BG_BASE/SECONDARY/PANEL/CARD`, `TEXT_PRIMARY/SECONDARY/MUTED`) и UI-утилитами (`COLOR_AXIS_TEXT`, `COLOR_EDGE`, `COLOR_EDGE_GONE`, `COLOR_SELECTED`, `COLOR_SCRUBBER`, `COLOR_RANGE`, diff-pastel цвета). Все hardcoded hex убраны из `ContentionHeatmap.tsx`, `FlameGraph.tsx`, `DependencyGraph.tsx`, `LifetimeBar.tsx`, `RequestsView.tsx`, `MetricsChart.tsx`, `TimelineCanvas.tsx`, `TimelineHeatmapCanvas.tsx`. `grep -r "#[0-9a-fA-F]{6}" web/src --include="*.tsx"` возвращает 0 результатов.

---

### ~~DS-4. Inline style cleanup~~ — ✅ РЕАЛИЗОВАНО (DS-4)

> **Реализовано:** Добавлены CSS-переменные для state colors (`--color-running/blocked/waiting/syscall/done/range`) в `:root`. Классы `.metrics-legend-dot--running/blocked/range` в index.css. В `MetricsChart.tsx` 3 inline `style={{ color: ... }}` заменены на className. Justified inline styles (canvas sizing, dynamic widths, tooltip позиционирование, градиенты) сохранены. `vite build` проходит.

---

### DS-5. Button/Badge API consolidation (P1)

**Gap:** 11 вариантов кнопок и 8 вариантов бейджей, каждый стилизован ad-hoc. Нет единого паттерна — сложно добавлять новые варианты и поддерживать консистентность.

**Задача:** Свести к базовым классам `.btn` (variants: primary, secondary, ghost, danger) и `.badge` (variants: state, severity, count). Обновить все вхождения в TSX. Удалить устаревшие CSS-правила.

**Критерий готовности:** Все кнопки используют `.btn .btn--{variant}`. Все бейджи — `.badge .badge--{variant}`. Визуально идентично текущему.

---

### DS-6. CSS split — index.css → feature files (P2)

**Gap:** `index.css` — 3354 строки, один монолит. Секция Analysis panel занимает 743 строки (22%). Невозможно найти стили конкретного компонента без поиска по всему файлу.

**Задача:** Разбить `index.css` по feature-файлам: `topbar.css`, `timeline.css`, `inspector.css`, `analysis-panel.css`, `goroutine-list.css`, `palette.css`, `theme.css` и т.д. Импортировать через `main.tsx` или Vite.

**Критерий готовности:** `index.css` ≤ 200 строк (только global reset + design tokens). Каждый feature-файл ≤ 400 строк. `vite build` проходит.
