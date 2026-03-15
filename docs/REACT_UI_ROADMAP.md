# React UI Roadmap

План переноса недостающих фич из vanilla UI в React UI.

## Текущее состояние

### Уже реализовано
- Copy link, Refresh
- Фильтры: пресеты (All, Blocked, Channels, Mutex), Search, State, Reason, Resource, Min wait, Sort, Clear
- Summary bar: Long blocked (кликабельно), Deadlock hints
- Inspector: Copy stack, Copy ID, Spawn tree
- Resource Graph
- Export JSON
- Клавиатура: Ctrl+G, ↑↓
- Синхронизация URL с фильтрами

### Отсутствует
| # | Фича | Сложность | Описание |
|---|------|-----------|----------|
| 1 | Save PNG | Низкая | Экспорт timeline в PNG |
| 2 | Zoom to G | Средняя | Зум timeline на выбранную горутину |
| 3 | Reset zoom | Низкая | Сброс уровня зума |
| 4 | Fullscreen | Низкая | Полноэкранный режим для timeline |
| 5 | Minimap | Высокая | Миникарта обзора при зуме |
| 6 | Related focus | Средняя | Фокус на связанных горутинах |
| 7 | Heatmap view | Высокая | Переключение lanes ↔ heatmap |
| 8 | Canvas timeline | Высокая | Zoom, pan, drag на canvas |

---

## План реализации

### Фаза 1: Простые фичи (1–2 дня)

#### 1.1 Save PNG
- [x] Добавить кнопку "Save PNG" в timeline-controls
- [x] Использовать `html2canvas` для рендера timeline в PNG

#### 1.2 Fullscreen
- [x] Добавить кнопку ⛶ в timeline-controls
- [x] Обработчик `requestFullscreen()` / `exitFullscreen()`
- [x] Обернуть timeline-panel в ref для fullscreen
- [x] Слушать `fullscreenchange` для обновления состояния кнопки

#### 1.3 Reset zoom
- [x] Добавить кнопку "Reset zoom" (показывать при zoomToSelected)
- [x] Сброс zoom state

#### 1.4 Subtitle
- [x] Добавить подзаголовок под заголовком: "Inspect goroutines, blocking behavior, and stack snapshots on a live runtime timeline."

---

### Фаза 2: Zoom и навигация (2–3 дня)

#### 2.1 Zoom to G
- [x] Добавить кнопку "Zoom to G"
- [x] При клике: вычислить временной диапазон сегментов выбранной горутины
- [x] Масштабировать ось времени на этот диапазон (div-based)

#### 2.2 Canvas-based Timeline с zoom/pan
- [ ] Заменить div-based Timeline на canvas (по аналогии с vanilla `app.js`)
- [ ] Реализовать: `timelineView = { zoomLevel, panOffsetNS }`
- [ ] Wheel: zoom in/out с центром на курсоре
- [ ] Mousedown/mousemove/mouseup: drag для pan при zoom > 1
- [ ] Hit-test на mousemove для tooltip с state/reason/duration
- [ ] Рендер сегментов с учётом zoom/pan

---

### Фаза 3: Продвинутые фичи (3–5 дней)

#### 3.1 Minimap
- [x] Полоска с индикатором viewport при zoom (div-based)
- [x] Показывать при Zoom to G
- [ ] Drag по minimap для навигации (требует canvas/pan)

#### 3.2 Related focus
- [x] Кнопка "Related focus"
- [x] Логика: горутины, связанные с выбранной (resource edges, parent/children)
- [x] Фильтр списка и timeline только связанных
- [x] Toggle on/off

#### 3.3 Heatmap view
- [ ] Кнопка "⊞ Heatmap"
- [ ] Режим: lanes (по умолчанию) vs heatmap
- [ ] Heatmap: GMP strip + пиксельная карта состояний горутин по времени
- [ ] Портировать логику из vanilla `renderHeatmap` / `renderGMPStrip`

---

## Зависимости

```
Фаза 1 (независима)
    ↓
Фаза 2: Zoom to G зависит от 2.2 (canvas + zoom state)
    ↓
Фаза 3: Minimap зависит от 2.2; Heatmap — отдельный режим рендера
```

## Референсы

- Vanilla UI: `internal/api/ui/app.js` (строки ~1950–2900 — timeline, zoom, minimap, heatmap)
- Vanilla styles: `internal/api/ui/styles.css` (`.minimap-canvas`, `.timeline-control-button`, etc.)
- API: `/api/v1/timeline`, `/api/v1/processor-timeline` (для GMP strip)

## Примечания

- При портировании canvas-логики можно вынести общие вычисления (метрики, сегменты) в хуки/утилиты
- Рассмотреть `useRef` для canvas, `useEffect` для подписки на resize/wheel/mouse
- Сохранить совместимость с текущим простым Timeline до полной готовности canvas-версии
