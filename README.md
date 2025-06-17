# ctlstream

A simple [Certificate Transparency Log](https://en.wikipedia.org/wiki/Certificate_Transparency) streaming implementation. It fetches all known usable logs from a [list maintained by Google](https://www.gstatic.com/ct/log_list/v3/all_logs_list.json) and monitors those logs for new certificate issuances. The output is streamed via a websocket in JSON format, available to consume using any client of your choice.

# Installation

Binaries for Linux, OSX and Windows can be found on the [latest release](https://github.com/thoaid/ctlstream/releases/latest) page. 

# Usage

A websocket will be exposed via `localhost:8080/ws`. You can connect to it using any client of your choice. For example, using `websocat`:

```
$ websocat ws://localhost:8080/ws | head -n3
{"subject":"CN=strategylogics.com","issuer":"CN=R11,O=Let's Encrypt,C=US","not_before":"2025-06-15T14:08:54Z","not_after":"2025-09-13T14:08:53Z","source":"DigiCert 'Sphinx2025h2' Log","timestamp":1750000048}
{"subject":"CN=www.vdi.navalgijon.es","issuer":"CN=R11,O=Let's Encrypt,C=US","not_before":"2025-06-15T13:57:20Z","not_after":"2025-09-13T13:57:19Z","source":"DigiCert 'Sphinx2025h2' Log","timestamp":1750000048}
{"subject":"CN=www.m.notturni.com","issuer":"CN=R10,O=Let's Encrypt,C=US","not_before":"2025-06-15T14:08:52Z","not_after":"2025-09-13T14:08:51Z","source":"DigiCert 'Sphinx2025h2' Log","timestamp":1750000048}
```
Certificates are included in the output by default. If you only care about metadata, pass the `-nocert` flag when starting the server.


# Public instance

A public instance of ctlstream can be found at: `wss://ctlstream.interrupt.sh/stream`