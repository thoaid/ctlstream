# ctlstream

A simple [Certificate Transparency Log](https://en.wikipedia.org/wiki/Certificate_Transparency) streaming implementation. It fetches all known usable logs from a [list maintained by Google](https://www.gstatic.com/ct/log_list/v3/all_logs_list.json) and monitors those logs for new certificate issuances. The output is streamed via a websocket in JSON format, available to consume using any client of your choice.

# Installation

Binaries for Linux, OSX and Windows can be found on the [latest release](https://github.com/thoaid/ctlstream/releases/latest) page. 

# Usage

* Certificates are included in the output by default. If you only care about metadata, pass the `-nocert` flag when starting the server.

* A websocket will be exposed via `:8080/ws`. You can connect to it using any client of your choice. For example, using `websocat`:

```
$ websocat ws://localhost:8080/ws
{"subject":{"cn":"jessy-yung-couverture.com","o":null,"ou":null,"c":null,"raw":"CN=jessy-yung-couverture.com"},"issuer":{"cn":"R11","o":["Let's Encrypt"],"ou":null,"c":["US"],"raw":"CN=R11,O=Let's Encrypt,C=US"},"not_before":"2025-06-19T19:30:14Z","not_after":"2025-09-17T19:30:13Z","source":"Google 'Argon2025h2' log","timestamp":1750364999}
{"subject":{"cn":"api.biolifrplasma.com","o":null,"ou":null,"c":null,"raw":"CN=api.biolifrplasma.com"},"issuer":{"cn":"R11","o":["Let's Encrypt"],"ou":null,"c":["US"],"raw":"CN=R11,O=Let's Encrypt,C=US"},"not_before":"2025-06-19T19:30:18Z","not_after":"2025-09-17T19:30:17Z","source":"Google 'Argon2025h2' log","timestamp":1750364999}
{"subject":{"cn":"www.mx01.expo-realestate.com","o":null,"ou":null,"c":null,"raw":"CN=www.mx01.expo-realestate.com"},"issuer":{"cn":"R11","o":["Let's Encrypt"],"ou":null,"c":["US"],"raw":"CN=R11,O=Let's Encrypt,C=US"},"not_before":"2025-06-19T19:30:18Z","not_after":"2025-09-17T19:30:17Z","source":"Google 'Argon2025h2' log","timestamp":1750364999}
...
```

* The output is simple JSON. You can pipe it into tools like `jq` or save it to a file for post-processing:

```
$ websocat ws://localhost:8080/ws | jq '.subject.cn'
"www.scc-ny.com"
"www.teamhustlemovement.com"
"nostalgie-shop.de"
...
```

# Public instance

A public instance of ctlstream can be found at: `wss://ctlstream.interrupt.sh/stream`