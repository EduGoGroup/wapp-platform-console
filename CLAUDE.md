# CLAUDE.md — `wapp-platform-console`

Portal corto. **La verdad vive en [`documentations/`](documentations/README.md)**; esto solo apunta.

## Qué es esta pieza
La consola web de **operadores de plataforma de wApp** — *nosotros*, el staff. Escucha en
`127.0.0.1:8106`. Da de alta empresas, emite códigos de enrolamiento de Edge, **corta y restaura el
servicio de un cliente** (kill-switch comercial) y resuelve la bandeja de solicitudes de acceso. Go
+ Gin + `html/template` embebidas, **cero JavaScript**, **cero base de datos**: es un cliente HTTP
del listener admin `:8100`. Nació del Plan 056: ese kill-switch se operaba con `curl`.
🔴 **NO es la consola del cliente**: `wapp-client-console` (`:8107`) es la de la dueña del negocio
—sus pedidos, su catálogo, sus sesiones— y confundirlas es el error de lectura más probable del
ecosistema. Si la pantalla que buscas habla de pedidos o de WhatsApp, te has equivocado de repo.

## Las cinco reglas innegociables

1. **Esta consola NO es un perímetro de seguridad.** La cookie de sesión no está firmada y el JWT se
   lee con `ParseUnverified`: cualquiera puede fabricar una y navegar las pantallas. **El gate real
   es `:8100`**, que sí valida firma, RBAC y pertenencia. No cuelgues de aquí ninguna decisión de
   autorización, y no publiques este puerto: escucha en loopback a propósito.
2. **La excepción administrativa a INV-8 está AUTORIZADA por el ADR-0039** (repo de documentación
   del ecosistema). Esta pieza manda el tenant **objetivo en el cuerpo** —lo que el resto tiene
   prohibido— porque el que **ejecuta** sigue saliendo del token. La sostienen tres cercas del
   cloud: permisos con sufijo `.any`, un `deny '*.any'` sobre `tenant_admin` y la comprobación de
   pertenencia al tenant de plataforma en cada handler. Por eso `platform_admin` **no se ofrece** en
   la bandeja: sería escalada de privilegios.
3. **Zero-knowledge y doble llave.** La nube nunca ve credenciales ni llaves: la **DEK** (descifra
   el almacén de `whatsmeow`) la custodia el cliente y **jamás cruza ningún contrato**; el **Lease**
   lo emite y revoca el servidor y es el kill-switch **anti-clon**. Protege llaves, **no** el
   contenido de negocio, que sí sube a la nube a propósito. ⚠️ El corte que opera esta consola es el
   kill-switch **COMERCIAL por tenant**, no el del lease.
4. **Nada de infraestructura aquí, y nada de forks.** Sin base de datos, sin broker, sin Redis: la
   concurrencia se resuelve con Go. Lo transversal (CSRF, cabeceras, cookies, rate-limit, sesión)
   vive en **`wapp-shared`**, monorepo multi-módulo con releases por módulo (tags
   `<modulo>/vX.Y.Z`); aquí se consumen `web`, `iam`, `ui`, `auth` y `config`. Sus copias propias ya
   se borraron una vez.
5. **Copia-adaptación, nunca dependencia.** Parte del código se copió de EduGo y se adaptó al
   espacio de nombres de wApp: **prohibido importar un repo `edugo-*`**. Falso amigo: la
   organización se llama `EduGoGroup`, así que `github.com/EduGoGroup/wapp-shared/...` sí vale.

## Antes de tocar nada

- **`GOWORK=off` siempre**: lo llevan los targets del `Makefile`; ponlo tú si compilas a mano.
- **Un PR NO valida nada**: `ci.yml` es `workflow_dispatch`; el gate real es `make ci-local` y
  `make ci-docker`. **Cuenta los SKIP**: un `rc=0` los cuenta igual que los PASS.
- **Verifica por mutación** antes de fiarte de un test verde; muta con `cp`, nunca con
  `git checkout <fichero>`, que borra el trabajo sin *commitear*.
- **El TTL del código de enrolamiento viaja como `ttl`, no `ttl_seconds`**: `encoding/json` ignora
  claves desconocidas **sin error**. Y no hay `TODO`/`FIXME` en el repo: la deuda va a `deuda.md`.

## Índice de `documentations/`

| Documento | Qué contiene |
|---|---|
| [`README.md`](documentations/README.md) | Portal de la pieza y la diferencia con la consola del cliente. |
| [`constitucion.md`](documentations/constitucion.md) | 🔴 **Empieza aquí.** Invariantes, la excepción a INV-8, tecnología real, convenciones y las 12 trampas. |
| [`arquitectura.md`](documentations/arquitectura.md) | Capas, mapa de paquetes, middlewares en orden y diagramas. |
| [`contratos.md`](documentations/contratos.md) | Las 19 rutas que sirve, las 10 que consume, 24 variables de entorno y qué registra. |
| [`operacion.md`](documentations/operacion.md) | Arranque local, gates reales, publicación, depuración y el hueco del primer admin. |
| [`deuda.md`](documentations/deuda.md) | 22 entradas con `fichero:línea`, consecuencia y cierre. |
