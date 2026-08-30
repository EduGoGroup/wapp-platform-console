# `wapp-platform-console` — la consola de OPERADORES de wApp

## 🔴 Lo primero: esto NO es la consola del cliente

Esta consola es **nuestra**, la de los operadores de la plataforma wApp. **No** es la de la dueña
del negocio que usa WhatsApp. Confundirla con `wapp-client-console` es el error de lectura más
probable de todo el ecosistema, así que aquí va la diferencia antes que nada:

| | `wapp-platform-console` (**este repo**) | `wapp-client-console` (**otro repo**) |
|---|---|---|
| Quién la usa | **nosotros**, staff de wApp | **el cliente**: la dueña del negocio |
| Puerto | `127.0.0.1:8106` | `127.0.0.1:8107` |
| Qué opera | empresas ajenas: alta, corte y restauración del servicio, códigos de enrolamiento, bandeja de solicitudes de acceso | su propio tenant: sesiones, bandeja de pedidos, flujos, catálogo |
| Contra quién habla | el listener **admin** de la plataforma, `:8100` | la **API pública**, `:8103` |
| `system` ante identity | **`wapp.platform`** | `wapp.bff` |
| Cookies | `wapp_platform_session` / `wapp_platform_csrf` | las suyas, distintas a propósito |
| Alcance de sus permisos | permisos con sufijo **`.any`**: actuar sobre un tenant que no es el tuyo | permisos normales, acotados a su tenant |

Si estás tocando pantallas de negocio (pedidos, catálogo, flujos, sesiones de WhatsApp),
**te has equivocado de repo**.

## Qué es, en tres frases

Una consola web *server-side* en Go (Gin + `html/template` embebidas, **cero JavaScript**) que da
de alta empresas, emite códigos de enrolamiento de Edge, **corta y restaura el servicio de un
cliente** (kill-switch comercial) y resuelve la bandeja de solicitudes de acceso.
**No tiene base de datos**: es enteramente un cliente HTTP del listener admin `:8100`, y todo su
estado vive allí.
Nació del **Plan 056** porque ese kill-switch funcionaba y no tenía ninguna interfaz: se operaba
con `curl` y un UUID pegado a mano.

## Índice de `documentations/`

| Documento | Para qué |
|---|---|
| [`constitucion.md`](constitucion.md) | 🔴 **empieza aquí**. Los invariantes que no se pueden violar (los del ecosistema y los propios), la tecnología real del `go.mod`, las convenciones y **las trampas conocidas** de esta pieza. |
| [`arquitectura.md`](arquitectura.md) | Cómo está hecha por dentro: capas, mapa de paquetes, punto de entrada, y los diagramas. |
| [`contratos.md`](contratos.md) | Todo lo que otros consumen o le pasan: las 19 rutas HTTP que sirve, las 10 que consume de `:8100`, las variables de entorno con su default, y las tablas que mueve a distancia. |
| [`operacion.md`](operacion.md) | Cómo se arranca en local, cómo se prueba (los `make` reales), cómo se publica y cómo se depura. Incluye el **hueco del primer administrador**. |
| [`deuda.md`](deuda.md) | La deuda viva, con `fichero:línea`, consecuencia y cómo se cerraría. Incluye el código muerto verificado. |

## Estado del repo cuando se escribió esta documentación

- **2026-08-30**, rama `main`, HEAD `b89c803fe2729619c420a4ef9b1cd675696eea9f`.
- Único tag publicado: **`v0.1.0`** (apunta a `5e447dd`, **4 commits por detrás** del HEAD).
- 18 commits en total, del 2026-08-14 al 2026-08-28. `main`, `dev`, `origin/main` y `origin/dev`
  en el mismo SHA.
- **1.801 líneas de Go de producción** y 3.144 de test (ratio ≈ 1,75:1) en 30 ficheros `.go`.
  Es la pieza más pequeña del ecosistema.
- Desplegada en la máquina de UAT como unidad systemd `wapp-platform-console`, escuchando en
  `127.0.0.1:8106`; el binario vivo lleva empotrado ese mismo `b89c803`.
