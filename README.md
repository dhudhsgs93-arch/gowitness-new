# gowitness-new — gowitness v3.1.1 с review системой

Форк gowitness с добавленными тегами, комментариями и фильтрами для ревью скриншотов.

## Установка

```bash
# скопировать бинарник в PATH (один раз)
sudo cp gowitness-new /usr/local/bin/gowitness-new
```

## Запуск

```bash
# из директории где лежат gowitness.sqlite3 и screenshots/
gowitness-new report server

# или с явными путями
gowitness-new report server \
  --db-uri "sqlite://gowitness.sqlite3" \
  --screenshot-path ./screenshots \
  --port 7171
```

Открыть `http://127.0.0.1:7171` в браузере.

Все остальные команды gowitness (`scan`, `single`, `nmap` и т.д.) работают без изменений.

## Что добавлено

### Теги на каждой карточке

На каждом скриншоте в галерее — строка кнопок-тегов:

| Тег | Цвет | Назначение |
|-----|------|------------|
| ✓ Done | зелёный | отсмотрел, неинтересно |
| ⚠ Attention | красный | требует внимания, вернуться |
| ★ Interesting | жёлтый | интересный хост |
| ☠ Vuln | фиолетовый | найдена уязвимость |
| 🗑 Junk | серый | мусор (карточка затухает) |

Клик на активный тег снимает его. Цветная полоска слева показывает статус.

### Комментарии

Текстовое поле под каждой карточкой. Автосохранение через 0.8 сек после ввода.

### Фильтры

В тулбаре — пилюли с подсчётом по каждому статусу. Кликнуть = показать только хосты с этим тегом.

### Detail страница

При клике на скриншот — блок "Review" сверху левой колонки с кнопками тегов и полем комментария.

## Горячие клавиши

| Клавиша | Действие |
|---------|----------|
| `J` / `K` | навигация вниз / вверх по карточкам |
| `1` | ✓ Done |
| `2` | ⚠ Attention |
| `3` | ★ Interesting |
| `4` | ☠ Vuln |
| `5` | 🗑 Junk |
| `0` | снять тег |
| `C` | фокус на комментарий |
| `Esc` | выйти из комментария |
| `←` / `→` | предыдущая / следующая страница |

## API

```
GET  /api/review/stats          — статистика (кол-во по каждому статусу)
GET  /api/review/export         — markdown экспорт всех комментов и важных тегов
GET  /api/review/{id}           — получить review для хоста
POST /api/review/{id}           — {"status":"attention","comment":"текст"}
POST /api/review/bulk           — {"ids":[1,2,3],"status":"junk"}
GET  /api/results/gallery?review=attention  — фильтр галереи по статусу
```

Допустимые значения `status`: `done`, `attention`, `interesting`, `vuln`, `junk`, пустая строка (снять).

## Где хранятся данные

Таблица `reviews` создаётся автоматически в той же `gowitness.sqlite3`. Оригинальные данные gowitness не затрагиваются.

## Сборка из исходников

```bash
# клонировать gowitness
git clone --depth 1 --branch 3.1.1 https://github.com/sensepost/gowitness.git
cd gowitness

# применить патч (если есть .patch файл)
# git apply gowitness-review.patch

# собрать фронтенд
cd web/ui && npm install && npm run build && cd ../..

# собрать бинарник
CGO_ENABLED=0 go build -o gowitness-new .
```

Требования: Go 1.21+, Node 18+.
