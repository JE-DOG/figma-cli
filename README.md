# Figma CLI

Это **vibecoded CLI**, разработанный с помощью AI-агента.

Небольшой **read-only** CLI на Go: читает файл или поддерево Figma, показывает
исходные компоненты экземпляров, типографику, layout/геометрию и экспортирует PNG/SVG.
Работает через REST API без запущенного Figma Desktop, плагина и браузера.

Репозиторий, бинарник и skill называются **`figma-cli`**.

## Локальная установка и синхронизация

Bash-скрипт [`sync-local.sh`](tools/scripts/setup/sync-local.sh) пересобирает CLI
из текущего checkout, обновляет `~/.local/bin/figma-cli` и регистрирует skill
ссылкой в `~/.agents/skills/figma-cli`:

```sh
bash ~/projects/figma-cli/tools/scripts/setup/sync-local.sh
```

Запускайте его повторно после обновления исходников. Он работает из любого
рабочего каталога; нужен Go 1.26+, токен Figma не требуется. Скрипт не загружает
изменения из git: сначала обновите checkout удобным вам способом. В `~/.local/bin`
бинарник заменяется атомарно только после успешной сборки и проверки версии.
При ошибке сборки старая установка сохраняется. `bin/figma-cli` внутри checkout
этот скрипт не обновляет.

Если `~/.local/bin` ещё не входит в PATH, добавьте в настройки своей оболочки:

```sh
export PATH="$HOME/.local/bin:$PATH"
```

Для других каталогов или установки только бинарника:

```sh
bash tools/scripts/setup/sync-local.sh --bin-dir "$HOME/bin" --skills-dir "$HOME/.agents/skills"
bash tools/scripts/setup/sync-local.sh --no-skill
bash tools/scripts/setup/sync-local.sh --help
```

Существующая ссылка skill на этот checkout подходит для повторного запуска.
Чужой каталог, ссылка на другой checkout или сломанная ссылка вызывают ошибку
до сборки и замены бинарника; скрипт не удаляет их. Выберите другой
`--skills-dir` или используйте `--no-skill`. Настройки shell, токены и другие CLI
не меняются; `sudo` не требуется. Поддерживается Bash 3.2+.

Проверка сценариев установки в изолированных временных каталогах:

```sh
bash tools/scripts/setup/test-sync-local.sh
```

## Skill для Codex

Англоязычный skill [`figma-cli`](skills/figma-cli/SKILL.md) объясняет агенту,
как выбирать узлы, читать исходные компоненты и mixed typography, интерпретировать
layout и экспортировать PNG/SVG. Инструкции хранятся в этом репозитории;
`agents/openai.yaml` задаёт название для списка skills. README остаётся на русском.

Скрипт выше регистрирует skill вместе с CLI. Если нужна только регистрация
без сборки, создайте ссылку в пользовательском каталоге `~/.agents/skills`:

```sh
mkdir -p "$HOME/.agents/skills"
ln -s "$HOME/projects/figma-cli/skills/figma-cli" "$HOME/.agents/skills/figma-cli"
```

Если checkout находится в другом месте, замените путь к нему. Команда `ln -s`
не заменяет существующую регистрацию; перед повторной установкой проверьте её.
Ссылка использует актуальные инструкции из репозитория без копирования файлов.
Не перемещайте checkout без обновления ссылки. При запуске команд из другого
проекта skill определяет расположение `figma-cli` (включая установку через PATH)
и использует абсолютный путь к бинарнику.

Пример запроса в Codex:

```text
$figma-cli посмотри исходные компоненты, шрифты и отступы по ссылке FIGMA_URL
```

Skill также может выбираться автоматически по задаче. Если он не появился в
списке, перезапустите Codex. Токен передаётся самому CLI через `FIGMA_TOKEN` в
окружении процесса или `--token-stdin`; в skill и историю чата его добавлять не нужно.
Экспорт через skill выполняется, когда его запросили в задаче.

Расположение пользовательских skills и поддержка symlink описаны в
[официальной документации OpenAI](https://learn.chatgpt.com/docs/build-skills).

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
go build -o bin/figma-cli ./cmd/figma-cli
./bin/figma-cli help
```

`httptest`-тесты используют локальные HTTP/TLS-серверы и вымышленные данные;
необходим доступ к loopback. Они не обращаются к Figma и не требуют токена.
Эта команда сборки кладёт бинарник в игнорируемый `bin/`. Для установки в
пользовательский PATH используйте `tools/scripts/setup/sync-local.sh`, описанный выше.

## Доступ к Figma

Создайте personal access token в Figma → Settings → Security → Personal access
tokens с областью **`file_content:read`** и доступом к нужному файлу.
Токен можно передать через переменную `FIGMA_TOKEN` из secret manager либо через
`--token-stdin`. Файлы конфигурации и `.env` автоматически не читаются.

Пример для zsh: ввод скрыт, значение не попадает в историю команд.

```sh
read -rs 'FIGMA_TOKEN?Figma token: '
export FIGMA_TOKEN
./bin/figma-cli inspect 'https://www.figma.com/design/FILE_KEY/Screen?node-id=12-34'
unset FIGMA_TOKEN
```

Альтернатива — pipe из вашего secret manager:

```sh
your-secret-manager-command | ./bin/figma-cli inspect --token-stdin --node 12:34 FILE_KEY
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
./bin/figma-cli parse 'https://www.figma.com/design/FILE_KEY/Screen?node-id=12-34'

# Нормализовать URL IDs, выбрать несколько узлов
./bin/figma-cli parse --node 12-34 --node '56:78,90:12' FILE_KEY

# Отчёт по выбранному поддереву
./bin/figma-cli inspect --node 12:34 --depth 3 FILE_KEY

# Файл целиком; --max-nodes ограничивает отчёт, но не ответ API
./bin/figma-cli inspect --max-nodes 500 FILE_KEY

# Локальный size, relativeTransform и векторные paths из API
./bin/figma-cli inspect --geometry --node 12:34 FILE_KEY

# Точный ответ API со всеми неизвестными полями и исходными числами
./bin/figma-cli raw --node 12:34 FILE_KEY > response.json

# Получить конкретную версию
./bin/figma-cli inspect --file-version VERSION_ID --node 12:34 FILE_KEY

# Скачать PNG 2x; создаст exports/12-34.png
./bin/figma-cli export --node 12:34 --scale 2 FILE_KEY

# SVG по умолчанию переводит текст в outlines
./bin/figma-cli export --node 12:34 --format svg --output assets FILE_KEY

# Сохранить SVG <text>; вид зависит от шрифтов и рендерера
./bin/figma-cli export --node 12:34 --format svg --svg-text FILE_KEY

# Полные границы вместо обрезки текстового узла
./bin/figma-cli export --node 12:34 --absolute-bounds FILE_KEY
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
./bin/figma-cli inspect --node 12:34 FILE_KEY |
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
