# MailNotifyer

MailNotifyer nimmt E-Mails per SMTP aus einem vertrauenswürdigen internen Netz
entgegen und sendet daraus Benachrichtigungen an
[Pushover](https://pushover.net/). Angenommene Nachrichten werden vor der
SMTP-Bestätigung dauerhaft lokal gespeichert. Weitere Zustellziele können über
das kleine `delivery.Target`-Interface ergänzt werden.

## Funktionsumfang

- SMTP-/ESMTP-Listener auf Port `2525`
- CIDR-Allowlist auf Verbindungsebene
- persistente dateibasierte Queue mit atomaren Schreibvorgängen
- exponentielle Wiederholungen bei Netzwerk- und Pushover-5xx-Fehlern
- separates `failed`-Verzeichnis für dauerhafte Pushover-4xx-Fehler
- MIME-Parsing mit `text/plain` und HTML-zu-Text-Fallback
- UTF-8-sichere Pushover-Limits:
  - Titel/Betreff: maximal 250 Unicode-Zeichen
  - Nachricht: maximal 1024 Unicode-Zeichen
- JSON-Logs und externer Connectivity-Healthcheck
- Betrieb als unprivilegierter Benutzer in einem read-only Container

Anhänge werden beim MIME-Parsing ignoriert und nicht in der Queue gespeichert.
Die Pushover-Nachricht besteht aus Absender und Nachrichtentext; der Betreff wird
als Pushover-Titel verwendet.

## Schnellstart

1. Eine Pushover-Anwendung anlegen und Token sowie User-/Group-Key bereithalten.
2. Konfiguration erstellen:

   ```sh
   cp .env.example .env
   ```

3. In `.env` mindestens `PUSHOVER_TOKEN` und `PUSHOVER_USER` setzen.
4. Dienst starten:

   ```sh
   docker compose up -d --build
   ```

5. Testmail senden, beispielsweise mit `swaks`:

   ```sh
   swaks \
     --server 127.0.0.1:2525 \
     --from monitoring@example.local \
     --to alerts@example.local \
     --header "Subject: Testalarm" \
     --body "MailNotifyer funktioniert."
   ```

Das Container-Image führt alle 30 Sekunden `/mailnotifier healthcheck` aus.
Dieser Befehl prüft die externe HTTPS-Erreichbarkeit von Pushover. Dabei wird
keine Benachrichtigung versendet und es werden keine Pushover-Zugangsdaten
benötigt. Jeder empfangene HTTP-Status gilt als erfolgreiche
Netzwerkverbindung.

> **Netzwerksicherheit:** Compose bindet SMTP standardmäßig nur an
> `127.0.0.1`. Für andere interne Geräte sollte `SMTP_BIND_ADDRESS` auf die
> konkrete private Interface-IP gesetzt und zusätzlich per Host-Firewall
> eingeschränkt werden. Die CIDR-Allowlist ersetzt keine Firewall. Bei
> vorgeschaltetem NAT oder Proxy sieht MailNotifyer möglicherweise dessen
> Adresse; die Allowlist muss die tatsächlich beobachtete Quelladresse
> enthalten.
>
> Der gewählte interne Listener bietet bewusst weder SMTP AUTH noch TLS. E-Mails
> und Zugang zum Listener müssen daher durch ein isoliertes, vertrauenswürdiges
> Netz und die Allowlist geschützt werden. Für Verkehr über nicht
> vertrauenswürdige Netze sollte TLS/AUTH ergänzt oder ein gehärteter MTA
> vorgeschaltet werden.

## Konfiguration

| Variable | Standard | Beschreibung |
| --- | --- | --- |
| `PUSHOVER_TOKEN` | – | Pushover Application API Token, erforderlich |
| `PUSHOVER_USER` | – | Pushover User- oder Group-Key, erforderlich |
| `PUSHOVER_DEVICE` | leer | Optionales einzelnes Pushover-Gerät |
| `PUSHOVER_ENDPOINT` | Pushover API | Abweichender API-Endpunkt, primär für Tests |
| `PUSHOVER_TIMEOUT` | `10s` | HTTP-Timeout |
| `SMTP_ADDR` | `:2525` | Listener-Adresse im Container |
| `SMTP_DOMAIN` | `mailnotifier.local` | SMTP-Banner-Domain |
| `SMTP_ALLOWED_CIDRS` | private Netze + Loopback | Kommagetrennte Quellnetz-Allowlist |
| `SMTP_MAX_MESSAGE_BYTES` | `10485760` | Maximale Größe einer eingehenden Mail |
| `QUEUE_DIR` | `/data/queue` | Persistentes Queue-Verzeichnis |
| `HEALTHCHECK_URL` | `https://api.pushover.net/1/` | Externer Connectivity-Endpunkt |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn` oder `error` |

Die Compose-spezifischen Variablen `SMTP_BIND_ADDRESS` und `SMTP_PORT` steuern
nur die SMTP-Portfreigabe auf dem Host.

## Queue-Verhalten

Nach erfolgreichem MIME-Parsing wird nur die zielunabhängige Benachrichtigung
unter `/data/queue/pending/<id>.json` gespeichert. Erst nach erfolgreichem
`fsync` und atomarem Umbenennen bestätigt der SMTP-Server die Annahme. Das
Pushover-Target kürzt Titel und Nachricht erst unmittelbar vor seinem
API-Aufruf, damit spätere Targets weiterhin den vollständigen Text erhalten.

- Netzwerkfehler und HTTP-5xx: Wiederholung nach 5 Sekunden, dann exponentiell
  bis maximal einmal pro Stunde.
- HTTP-4xx einschließlich Pushover-Quota-Fehlern: keine unveränderte
  Wiederholung, da Pushover dies ausdrücklich als nicht erfolgreich
  dokumentiert. Das Queue-Element wird nach `/data/queue/failed` verschoben.
- Erfolgreiche Zustellung: Queue-Datei wird gelöscht.

Fehlgeschlagene Elemente werden absichtlich nicht automatisch gelöscht. Nach
Behebung der Ursache können sie geprüft und bei Bedarf manuell wieder nach
`pending` verschoben werden.

## Warum dieser SMTP-Listener?

Für den gewählten Einsatz im internen Netz ist
[`emersion/go-smtp`](https://github.com/emersion/go-smtp) die passendste
Lösung:

- aktiv gepflegte RFC-5321-Implementierung,
- direkte Einbettung in denselben Prozess wie Queue und Targets,
- kleiner statischer Container ohne separaten MTA,
- Größenlimits, Timeouts, SMTPUTF8 und sauberes Shutdown-Verhalten.

Alternativen:

- **Postfix** ist die bessere Wahl, wenn MailNotifyer künftig direkt als
  öffentlicher MX aus dem Internet empfangen soll. Es bietet eine
  kampferprobte Mail-Queue, umfangreiche Zugriffskontrollen und etablierte
  MTA-Härtung, erhöht für den internen Anwendungsfall aber Komplexität und
  Container-Größe.
- **Haraka** ist leistungsfähig und pluginorientiert, wäre hier jedoch eine
  zusätzliche Laufzeit plus eigener Queue-Plugin-Logik.
- **Mailpit/MailHog** sind primär Test-Mailserver und keine passende Grundlage
  für einen produktiven Benachrichtigungsdienst.

Ein Wechsel zu öffentlichem MX-Betrieb sollte deshalb nicht nur durch Öffnen
von Port 25 erfolgen, sondern mit Postfix als vorgeschaltetem MTA und Übergabe
an MailNotifyer per LMTP oder Pipe.

## Entwicklung

```sh
go test ./...
go vet ./...
docker build -t mailnotifier .
```

## CI, Sicherheit und Container-Veröffentlichung

Der Workflow unter `.github/workflows/ci.yml` folgt dem Muster von
CupNotifyer. Er läuft für Pull Requests, Pushes auf `main`, Versionstags,
manuelle Aufrufe und wöchentlich:

- Formatprüfung, Race-Tests, `go vet`, Binary-Build und `govulncheck`
- Dependency Review für Pull Requests
- CodeQL-Analyse
- Container-Build und Trivy-Scan ausschließlich bei Versionstags
- Veröffentlichung für `linux/amd64` und `linux/arm64` mit SBOM und Provenance
  nach `ghcr.io/<owner>/<repository>`

Ein semantischer Versionstag wie `v1.2.3` veröffentlicht Tags für die genaue
Version, Major/Minor, Commit-SHA und `latest`. Alle verwendeten Actions sind
auf Commit-SHAs fixiert. Dependabot prüft Go-Module, GitHub Actions und das
Docker-Basisimage wöchentlich.

Die wichtigsten Pakete:

- `internal/smtpserver`: SMTP-Annahme und Netzwerk-Allowlist
- `internal/mailparser`: MIME-Abbildung und Zeichenlimits
- `internal/queue`: persistente Zustellung und Backoff
- `internal/delivery`: erweiterbare Target-Schnittstelle
- `internal/target/pushover`: Pushover-Implementierung
