# Figma REST CLI

Небольшой **read-only** CLI на Go: читает файл или поддерево Figma, показывает
исходные компоненты экземпляров, типографику, layout/геометрию и экспортирует PNG/SVG.
Работает через REST API без запущенного Figma Desktop, плагина и браузера.

Репозиторий называется `figma-cli`, бинарник — **`figma-rest`**: он не заменяет
существующий `silships/figma-cli` и не меняет его skill или настройки.

## Почему Go

| Критерий | Go | Rust |
| --- | --- | --- |
| HTTP, JSON, аргументы, тестовые серверы | Стандартная библиотека | Обычно отдельные crates |
| Поставка | Один бинарник | Один бинарник |
| Ресурсы | GC, достаточный для сетевой утилиты | Точный контроль памяти без GC |
| Поддержка этого CLI | Небольшой объём кода без внешних зависимостей | Больше настройки для тех же REST-задач |

Выбран Go; при реализации использовался установленный Go 1.26.1. Rust имеет смысл,
если потребуется строгий контроль памяти или интеграция в существующий Rust-проект.

## Сборка и проверка

Нужен Go 1.26+. Внешних Go-зависимостей нет.

```sh
cd ~/projects/figma-cli
go test -race ./...
go vet ./...
go build -o bin/figma-rest ./cmd/figma-rest
./bin/figma-rest help
```

`httptest`-тесты используют локальные HTTP/TLS-серверы и вымышленные данные;
необходим доступ к loopback. Они не обращаются к Figma и не требуют токена.
Сборка кладёт бинарник в игнорируемый `bin/`; глобальная установка не выполняется.

## Доступ к Figma

Создайте personal access token в Figma → Settings → Security → Personal access
tokens с областью **`file_content:read`** и доступом к нужному файлу.
Токен можно передать через переменную `FIGMA_TOKEN` из secret manager либо через
`--token-stdin`. Файлы конфигурации и `.env` автоматически не читаются.

Пример для zsh: ввод скрыт, значение не попадает в историю команд.

```sh
read -rs 'FIGMA_TOKEN?Figma token: '
export FIGMA_TOKEN
./bin/figma-rest inspect 'https://www.figma.com/design/FILE_KEY/Screen?node-id=12-34'
unset FIGMA_TOKEN
```

Альтернатива — pipe из вашего secret manager:

```sh
your-secret-manager-command | ./bin/figma-rest inspect --token-stdin --node 12:34 FILE_KEY
```

Здесь `your-secret-manager-command` — заменяемый пример, а не зависимость CLI.
stdin читается до EOF, максимум 8 KiB; этот флаг явно заменяет `FIGMA_TOKEN`.
Для уже полученного OAuth access token используйте `--auth oauth`; создание
OAuth-приложения, вход и обновление токенов в CLI не реализованы.
По умолчанию PAT отправляется в `X-Figma-Token`, OAuth — в `Authorization: Bearer`.
Флага токена в аргументах нет. Токены не сохраняются, не печатаются в диагностике,
не передаются CDN при скачивании. В API выполняются только GET-запросы.

## Использование

Все флаги ставятся **перед** последним аргументом. Замените `FILE_KEY` и `12:34`
своими значениями. URL нужно заключать в кавычки из-за `?`, `&` и `;`.

```sh
# Разобрать ссылку офлайн, без токена
./bin/figma-rest parse 'https://www.figma.com/design/FILE_KEY/Screen?node-id=12-34'

# Нормализовать URL IDs, выбрать несколько узлов
./bin/figma-rest parse --node 12-34 --node '56:78,90:12' FILE_KEY

# Отчёт по выбранному поддереву
./bin/figma-rest inspect --node 12:34 --depth 3 FILE_KEY

# Файл целиком; --max-nodes ограничивает отчёт, но не ответ API
./bin/figma-rest inspect --max-nodes 500 FILE_KEY

# Локальный size, relativeTransform и векторные paths из API
./bin/figma-rest inspect --geometry --node 12:34 FILE_KEY

# Точный ответ API со всеми неизвестными полями и исходными числами
./bin/figma-rest raw --node 12:34 FILE_KEY > response.json

# Получить конкретную версию
./bin/figma-rest inspect --file-version VERSION_ID --node 12:34 FILE_KEY

# Скачать PNG 2x; создаст exports/12-34.png
./bin/figma-rest export --node 12:34 --scale 2 FILE_KEY

# SVG по умолчанию переводит текст в outlines
./bin/figma-rest export --node 12:34 --format svg --output assets FILE_KEY

# Сохранить SVG <text>; вид зависит от шрифтов и рендерера
./bin/figma-rest export --node 12:34 --format svg --svg-text FILE_KEY

# Полные границы вместо обрезки текстового узла
./bin/figma-rest export --node 12:34 --absolute-bounds FILE_KEY
```

Принимаются file/branch key и HTTPS-ссылки `figma.com` / `www.figma.com` с путями
`file`, `design`, `proto`, `board`, `slides`. Это извлечение ключа, а не обещание
поддержки всех возможностей редактора через REST. В ссылке
`/design/MAIN_KEY/branch/BRANCH_KEY/Name` для API выбирается `BRANCH_KEY`.
Флаги `--node` заменяют `node-id` из URL; повторы удаляются с сохранением порядка.
Поддерживаются обычные пары ID и составные instance ID вроде `I12:34;56:78`;
`12-34` нормализуется в `12:34`. REST не публикует полную грамматику ID,
неизвестные будущие формы могут потребовать обновления парсера.

Вывод `parse`, `inspect` и `export` — форматированный JSON. `raw` возвращает
JSON API без преобразования полей и чисел. Диагностика идёт в stderr; ошибка
даёт exit code 1. Для отсутствующего/null узла `inspect` завершается ошибкой;
`raw` сохраняет исходный ответ, включая null.

## Содержание отчёта

`nodes` — плоский список в порядке обхода, с `id`, `name`, `type`, `parentId`.
Корневой родитель в ответе node API неизвестен. Пересекающиеся поддеревья
не дублируют один и тот же ID.

- `component` у INSTANCE связывает `componentId` с таблицей `components`:
  исходное имя, key, remote и имя component set, если метаданные доступны.
  Переименованный instance сохраняет своё `name`; имя источника находится в
  `component.name`. При нехватке метаданных `resolved=false`, без подмены именем
  экземпляра и без дополнительных запросов к библиотекам. Detached instance
  больше не содержит надёжной ссылки на исходный компонент.
- `typography` для TEXT/TEXT_PATH сохраняет базовый `style` целиком, включая
  font family/PostScript name, размер, начертание/weight, spacing и line height.
  `runs` объединяют базовый стиль с частичными overrides, каждый с текстом,
  override ID и полуоткрытыми `start`/`end`. Нулевой ID и отсутствующий хвост
  массива означают базовый стиль. Если override отсутствует в таблице,
  добавляется предупреждение и сохраняется базовый стиль для run.
- `layout` содержит переданные API padding, gaps, auto-layout и grid настройки,
  sizing/alignment, constraints, min/max, bounding boxes, радиусы и опциональную
  векторную геометрию. Отсутствующие поля не подменяются нулями; `0` и `null`
  сохраняются. `boundVariables` содержит ссылки, а не вычисленные значения.
- `units`, `limitations`, `warnings`, `truncated` делают ограничения явными.

Пример выборки через необязательный `jq`:

```sh
./bin/figma-rest inspect --node 12:34 FILE_KEY |
  jq '.nodes[] | select(.component) | {name, source: .component.name}'
```

## Единицы и границы применимости

- Геометрия, padding/gaps, font size и letter spacing — **пиксели Figma**.
  Пересчёт в Android dp/sp требует соглашения о масштабе макета и настройках
  платформы и не выполняется автоматически.
- `absoluteBoundingBox` — bounds в canvas после трансформаций;
  `absoluteRenderBounds` учитывает визуальные эффекты и может быть null.
  `size` и `relativeTransform` запрашиваются через `--geometry`; у повёрнутых
  узлов размер bounding box отличается от локального размера.
- Padding/gaps — настройки layout, а не вычисленные расстояния между любыми
  слоями. SPACE_BETWEEN, wrap, grid и вложенные transforms требуют layout engine;
  произвольные margins и расстояния CLI не додумывает.
- `lineHeightUnit` из REST: `PIXELS`, `FONT_SIZE_%`, `INTRINSIC_%`. Сохраняются
  соответствующие исходные числовые поля, включая deprecated `lineHeightPercent`;
  `AUTO` не синтезируется. `FONT_SIZE_%` и `INTRINSIC_%` различаются.
- Для mixed text выбрана индексация **UTF-16**, как в Figma Plugin API.
  REST описывает «characters», но явно не закрепляет UTF-16; это предположение
  требует проверки на живом REST-ответе с emoji. Fixtures проверяют эту модель,
  короткие override-массивы и surrogate pairs. Граница, разрезающая surrogate
  pair, вызывает предупреждение: весь символ получает стиль первой единицы.
- SVG text зависит от установленных шрифтов. PNG scale: 0.01–4; для SVG scale
  остаётся 1. Figma может уменьшить экспорт свыше 32 мегапикселей. Null URL
  рендера означает ошибку узла; все такие результаты проверяются до скачивания.

## Лимиты, ошибки и хранение

По умолчанию `--timeout 30s` ограничивает всю сетевую команду: запросы, ожидания
и последовательные скачивания. Ctrl+C отменяет запрос/ожидание.
`--retries 2` задаёт повторы только для 429 (допустимо 0–5). Учитываются
`Retry-After` в секундах и HTTP-date; отсутствующий/некорректный заголовок даёт
1/2/4… секунд. Если ожидание не помещается в timeout, команда возвращает ошибку
со сроком ожидания. Другие HTTP-ошибки не повторяются автоматически.

Лимиты: 100 выбранных IDs, JSON до 64 MiB, один asset до 128 MiB.
В `inspect` текст одного узла ограничен 256 KiB в UTF-8, а весь отчёт —
10000 style runs. Превышение даёт явную ошибку до разрастания отчёта в памяти:
выберите меньшее поддерево либо используйте `raw` для полного ответа.
`--max-nodes` по умолчанию 2000 (диапазон 1–100000) ограничивает только обход
отчёта с явным `truncated=true`. `--depth 0` означает отсутствие API-ограничения
глубины; 1–1000 передаётся в API. У file и nodes endpoints глубина отсчитывается
по-разному — см. документацию. Для больших файлов выбирайте узлы и `--depth`.
`raw` не применяет `--max-nodes`, но сохраняет лимит размера HTTP-ответа.

Тела HTTP-ошибок и signed CDN URLs не выводятся. При 401/403 проверяйте срок
токена, scope и доступ; при 404 — файл/ветку. Лимиты Figma зависят от плана,
ресурса и seat: увеличение числа повторов не повышает квоту.

Экспорт скачивается во временный файл в каталоге назначения и публикуется
атомарно без замены существующего файла. Файловая система должна поддерживать
hard links. Ошибка отдельного скачивания не оставляет его частичный файл;
ранее завершённые экспорты остаются. Manifest содержит только ID и локальный путь.
API-ответы и изображения могут содержать приватный дизайн: выбирайте место
хранения осознанно. `exports/`, `bin/` и `.env*` игнорируются git.

## Официальные источники

Проверены при реализации 25 октября 2026 года:

- [File/nodes/images endpoints, depth, null nodes и image scale](https://developers.figma.com/docs/rest-api/file-endpoints/)
- [Типы узлов: INSTANCE, TEXT и characterStyleOverrides](https://developers.figma.com/docs/rest-api/file-node-types/)
- [TypeStyle, единицы и component metadata](https://developers.figma.com/docs/rest-api/file-property-types/)
- [PAT и X-Figma-Token](https://developers.figma.com/docs/rest-api/personal-access-tokens/)
- [Authentication и scopes](https://developers.figma.com/docs/rest-api/authentication/)
- [Rate limits и Retry-After](https://developers.figma.com/docs/rest-api/rate-limits/)
- [Plugin API: UTF-16 и surrogate pairs](https://developers.figma.com/docs/plugins/api/properties/TextNode-deletecharacters/)
- [Официальный MCP guide: file/branch URL parsing](https://github.com/figma/mcp-server-guide/blob/main/skills-figquery/figma-code-connect/SKILL.md)

Проверки без токена подтверждают локальную реализацию. Для live smoke test
нужны ваш токен в env/stdin и URL доступного файла; live-проверка без них
не выполнялась.
