# meshcore-kit 🧩📻

**Общий рантайм приложений для устройств MeshCore: подключение, синхронизация, инбокс, keepalive — и сообщения, которые всегда влезают.**

*[🇬🇧 English version](README.md)*

`meshcore-kit` — это то, что нужно каждому приложению поверх
[meshcore-go](https://github.com/stempit/meshcore-go), но что никто не хочет
писать дважды: открытие соединения с устройством (BLE или serial), выгрузка
сообщений, накопленных пока приложение не работало, форвардинг живых
сообщений в ограниченный инбокс с курсорами и поддержание соединения. Пакет
`fit` решает вторую всеобщую проблему — упаковку ответов в лимит меша
140 байт.

## Пакеты

### `companion`

- `Connect(ConnectOptions)` — BLE-скан по умолчанию, USB serial по запросу.
- `Service` — стартовая синхронизация сохранённых сообщений (ограничена по
  времени), живой форвардер в инбокс и цикл keepalive/реконнекта каждые 30 с.
- `Inbox` — ограниченная (1000) FIFO нормализованных DM/канальных сообщений с
  курсорами: `Add`, `Drain`, `Since`, `LastID`. JSON-форма (`Message`) — то,
  что шлюзы отдают своим клиентам.

```go
client, err := companion.Connect(companion.ConnectOptions{Type: "serial", SerialPort: "/dev/ttyUSB0"})
svc := companion.NewService(client, slog.Default())
svc.Start(ctx) // блокирует на время стартовой синхронизации, дальше служит в фоне
msgs, cursor := svc.Inbox.Drain()
```

Один `Service` на физическое устройство: companion-устройство обслуживает
одно соединение, не запускайте два приложения на одно радио.

### `fit`

Упаковывает логические ответы в готовые к отправке меш-сообщения — чистая
функция без конфигов и провайдеров:

- варианты: `truncate` (безопасная по рунам обрезка + многоточие), `split`
  (`[i/N]` части, ≤9), `skip` (выбросить то, что не влезает), `llm` (ваш
  инжектируемый компрессор с автоматическим фолбэком на split),
- без потерь сжимающая замена кириллических букв на латинские омоглифы
  (идентичные глифы, вдвое меньше байт),
- работа с префиксом: префикс отправки попадает только на первое сообщение и
  учитывается в его бюджете.

```go
o := fit.Options{MaxBytes: 140, Ellipsis: "…", Variant: fit.Split}
parts := fit.Fit(o, "@[user] ", []string{longReply}) // каждая часть ≤ 140 байт
```

Приложения, которые разрешают политику из конфига, держат тонкую обёртку
(см. `internal/shrink` в meshcore-go-bot); алгоритмы живут здесь.

## Родственные проекты

- [meshcore-go](https://github.com/stempit/meshcore-go) — драйвер протокола,
  на котором построен этот рантайм.
- [meshcore-go-bot](https://github.com/stempit/meshcore-go-bot) — полноценный чат-бот в меше.
- [meshcore-http-gateway](https://github.com/stempit/meshcore-http-gateway),
  [meshcore-mcp-gateway](https://github.com/stempit/meshcore-mcp-gateway) —
  шлюзы на этом рантайме.

## Лицензия

MIT — см. [LICENSE](LICENSE).
