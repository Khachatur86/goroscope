# Задачи (Tasks)

Понятные задачи для выполнения плана. **Для новых фич — TDD**: сначала тест, потом реализация.

---

## Фаза 1: Релиз и документация (без TDD)

### Task 1.1: Релиз v0.1.0
- [x] Обновить CHANGELOG: `[Unreleased]` → `[0.1.0]` с датой
- [ ] Создать тег `v0.1.0`: `git tag v0.1.0 && git push origin v0.1.0`

### Task 1.2: Обновить REACT_UI_ROADMAP.md
- [x] Удалить таблицу «Отсутствует» или пометить все пункты как выполненное
- [x] Добавить примечание: «Все фичи из roadmap реализованы»

### Task 1.3: Обновить MVP_SPEC.md §16
- [x] Удалить или переписать «Immediate Next Steps» (устарело)

---

## Фаза 2: Benchmarks в CI (без новой логики)

### Task 2.1: Добавить benchmarks в CI
- [x] В `.github/workflows/ci.yml` добавить шаг:
  ```yaml
  - name: Benchmarks
    run: go test -bench=. -benchmem -count=1 ./internal/tracebridge/... ./internal/analysis/...
  ```
- [x] То же в `.gitlab-ci.yml`

---

## Фаза 3: Расширить тесты (подготовка к TDD)

### Task 3.1: Тесты для internal/cli
- [x] Создать `internal/cli/app_test.go`
- [x] Тест: `check` возвращает exit 1 при deadlock hints
- [x] Тест: `check` возвращает exit 0 при отсутствии hints
- [x] Тест: `version` выводит версию в stdout

### Task 3.2: Тесты для internal/api
- [x] Тест `handleReplayLoad`: POST multipart с .gtrace, проверка 200 и загрузки
- [x] Тест `handleGoroutineStackAt`: GET с ns, проверка stack в ответе

---

## Фаза 4: Export CSV/JSON (TDD)

### Task 4.1: goroscope export — тесты первыми
- [ ] **Red**: Написать тест `TestExportCommand_CSV` — запуск `goroscope export --format=csv capture.gtrace`, проверка stdout содержит заголовок и строки
- [ ] **Red**: Написать тест `TestExportCommand_JSON` — аналогично для json
- [ ] **Green**: Реализовать `export` command в cli
- [ ] **Refactor**: Вынести логику экспорта в пакет

### Task 4.2: Формат CSV
- [ ] Тест: колонки `goroutine_id,state,start_ns,end_ns,reason,resource_id`
- [ ] Реализация: итерация по сегментам, запись CSV

### Task 4.3: Формат JSON
- [ ] Тест: валидный JSON, структура `{ "segments": [...] }`
- [ ] Реализация: сериализация сегментов

### Task 4.4: Документация export
- [ ] README: секция `goroscope export`
- [ ] Пример: Python/pandas анализ CSV

---

## Фаза 5: Frontend-тесты (TDD)

### Task 5.1: Настроить Vitest
- [ ] `npm install -D vitest @testing-library/react @testing-library/dom jsdom`
- [ ] `web/vitest.config.ts`
- [ ] Обновить `package.json`: `"test": "vitest run"`

### Task 5.2: Smoke-тесты компонентов
- [ ] **Red**: Тест `Filters` — рендер без падения
- [ ] **Green**: Мок данных, `render(<Filters ... />)`
- [ ] **Red**: Тест `Inspector` — рендер с goroutine
- [ ] **Green**: Реализация
- [ ] **Red**: Тест `Timeline` — рендер с segments
- [ ] **Green**: Реализация

### Task 5.3: CI для frontend-тестов
- [ ] В CI после `make web` добавить `cd web && npm run test`

---

## Фаза 6: go test -trace поддержка (TDD, опционально)

### Task 6.1: Загрузка трейса из go test
- [ ] **Red**: Тест — `go test -trace=trace.out ./pkg` создаёт trace.out, `goroscope replay trace.out` загружает
- [ ] **Green**: Проверить совместимость формата; при необходимости адаптер
- [ ] Документация: «Без agent: go test -trace && goroscope replay»

---

## Порядок выполнения

```
1.1 → 1.2 → 1.3  (релиз + docs)
2.1              (benchmarks CI)
3.1 → 3.2        (расширить тесты)
4.1 → 4.2 → 4.3 → 4.4  (export, TDD)
5.1 → 5.2 → 5.3  (frontend tests)
6.1              (go test -trace, если время)
```

---

## Чеклист TDD для новых фич

1. **Red**: Написать failing test
2. **Green**: Минимальный код для прохождения
3. **Refactor**: Улучшить без изменения поведения
4. Повторить
