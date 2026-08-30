# Operación de `wapp-platform-console`

Cómo se arranca, se prueba, se publica y se depura. Verificado el **2026-08-30**.

---

## 1 · Arranque en local

### Lo mínimo

```bash
make run            # == GOWORK=off go run ./cmd/platform-console/main.go
```

Arranca en `127.0.0.1:8106` con **todos los defaults**, log en texto y nivel `debug` (porque el
ambiente por defecto es `local`). Verás una línea `arrancando consola de plataforma` y otra
`consola de plataforma escuchando`.

🔴 **Copiar `.env.example` a `.env` NO configura nada.** El lector de configuración solo consulta
el entorno del proceso y **no hay `godotenv` en `go.sum`**. Si defines variables, expórtalas:

```bash
export WAPP_ADMIN_API_BASE=http://127.0.0.1:8100
export WAPP_IDENTITY_URL=http://127.0.0.1:8200
export WAPP_PUBLIC_API_BASE=http://127.0.0.1:8103
make run
```

O carga el fichero tú mismo: `set -a; . ./.env; set +a; make run`.

🔴 **`GOWORK=off` no es opcional.** Todos los targets del `Makefile` lo llevan. Sin él arrastras el
`go.work` del árbol del ecosistema y dejas de probar contra las versiones **publicadas** de
`wapp-shared` que declara el `go.mod` — que es justo lo que este repo se compromete a consumir.

### Para que sirva de algo hace falta el resto

La consola **no tiene estado propio**: sin `:8100` vivo, la portada se pinta con el aviso «No se
pudieron cargar las empresas» y **status `200`**. Necesitas, como mínimo:

- `wapp-cloud-platform` escuchando en `:8100` (admin) y `:8103` (API pública), con su Postgres;
- acceso a **identity-core** (`:8200` en local; en UAT es un servicio remoto);
- una **cuenta de operador con acceso a `wapp.platform`** — ver §4, que es el punto que más
  tiempo cuesta.

### Comprobación rápida de que está arriba

```bash
curl -s http://127.0.0.1:8106/healthz
# {"status":"healthy","time":"2026-08-30T..."}
```

Ojo: `/healthz` está **detrás del rate-limit** (5 rps, burst 10). Un bucle de sondeo agresivo
recibe 429.

---

## 2 · Cómo se prueba

### 🔴 Antes de nada: un PR aquí NO valida nada

`.github/workflows/ci.yml` es **`on: workflow_dispatch`**. **No se dispara ni con push ni con
pull_request**: es el régimen local del ecosistema (decisión del dueño, 2026-08-01). El único
workflow que corre solo es `sync-main-to-dev.yml`, que **no valida nada**: solo deja `dev` alineada
con `main` tras publicar.

⇒ **El gate real es local, y es el `Makefile`.**

### 🔴 Y un `rc=0` no significa que se haya probado algo

`go test` devuelve `0` contando igual un `--- SKIP` que un `--- PASS`. En otros repos del
ecosistema los tests de integración **se saltan solos en silencio** cuando falta la variable de
base de datos (`WAPP_TEST_DB_DSN`) — llegaron a saltarse 22 de 31 sin que nada lo dijera. Así que
**cuenta los SKIP**, siempre:

```bash
GOWORK=off go test ./... -v 2>&1 | grep -c -- '--- SKIP'
```

✅ **Buena noticia específica de este repo: hoy ese número es 0.** Esta pieza **no tiene ninguna
dependencia externa en sus tests** — no hay Docker, ni Postgres, ni `WAPP_TEST_DB_DSN`, ni un solo
`t.Skip`. Todo se prueba con `httptest.NewServer` haciendo de `:8100` y cookies fabricadas a mano.
Es el único repo del ecosistema donde `go test ./...` a secas prueba todo lo que hay. **Vuelve a
contar los SKIP igualmente cuando añadas tests**: la propiedad se pierde en el primer `t.Skip`.

### Los targets reales y qué valida cada uno

| Target | Qué corre | Qué valida |
|---|---|---|
| `make fmt-check` | `gofmt -l .` | que no quede ningún fichero sin formatear (falla listándolos) |
| `make vet` | `go vet ./...` | errores de construcción que compilan pero están mal |
| `make lint` | `golangci-lint run` (**v2.12.2** fijada) | `errcheck` con `check-type-assertions`, `govet`, `ineffassign`, `staticcheck`, `unused`, `errorlint`, `errname`, `nilerr` + `gofmt`/`goimports` |
| `make test` | `go test -race ./...` | las **60** funciones `Test*`, incluida detección de carreras |
| `make build` | `go build ./...` | que compile todo, tests aparte |
| **`make ci-local`** | los cinco de arriba, en orden | **el gate de pre-push** |
| **`make ci-docker`** | `make ci-local` dentro de `golang:1.26.5-bookworm` | que el resultado no dependa de tu toolchain local |

**Los dos agregadores ven cosas distintas y ninguno basta solo.** `ci-local` usa el
`golangci-lint` que tengas instalado; `ci-docker` reproduce el toolchain fijado. Un commit puede
salir verde en uno y rojo en el otro. **Lee el código de retorno sin pipe** (un `| tee` te devuelve
el rc del `tee`, no el del test).

### Cobertura real, para que no te sorprenda

Los tests viven en `internal/web` (50 funciones) e `internal/adminclient` (10).
🔴 **`internal/bootstrap`, `internal/config` y `cmd/` no tienen ni un test**: 0 de 185 líneas.
En particular `config.Load()` y sus **tres alias legados** no los prueba nada.

### Los dos tests que conviene imitar

- `internal/web/theme_test.go` — **deriva** la lista de tokens sensibles al tema **del CSS del
  módulo pinado** en vez de copiarla, así que un release nuevo de `wapp-shared/ui` lo pone en rojo
  en vez de envejecer en silencio.
- `internal/web/catalog_test.go` — ata los `<option>` del HTML al catálogo real del cloud.
  ⚠️ Pero su candado es **de un solo sentido**: comprueba `ofrecido ⊆ sembrado`, nunca al revés, y
  por eso ya se le escapó un plan nuevo (ver [`deuda.md`](deuda.md)). Imita la idea, no el sentido.

### Verifica por mutación

Un test verde puede no estar mirando. La doctrina del ecosistema, pagada en campo: un test de
paginación con 12 filas seguía verde tras quitarle el `ORDER BY`. Antes de fiarte de un aserto,
rómpelo a propósito y comprueba que se pone rojo.
🔴 **Muta copiando el fichero (`cp`), nunca con `git checkout`**: un `git checkout <fichero>` sobre
trabajo sin *commitear* lo **borra entero**.

---

## 3 · Cómo se publica una versión

Este repo **no tiene `release.yml`**: el tag se corta a mano. Y **no es un módulo de
`wapp-shared`**, así que su tag es `vX.Y.Z` a secas, sin prefijo de módulo.

**Cadencia de ramas del ecosistema:** toda ola aterriza en **`dev`**; a **`main`** se pasa **al
final del plan**. Tras un push a `main`, `sync-main-to-dev.yml` realinea `dev` solo (usa el
`GITHUB_TOKEN` nativo, y por eso ese push **no** encadena otros workflows).

```bash
# 1. gate local, obligatorio, con el rc leído sin pipe
make ci-local ; echo "rc=$?"
make ci-docker ; echo "rc=$?"

# 2. dev → main (al cierre del plan, no antes)
git checkout main && git merge --no-ff dev

# 3. tag y push
git tag -a v0.1.1 -m "…" && git push origin main --tags
```

**Estado hoy:** único tag publicado **`v0.1.0`**, que apunta a `5e447dd` y va **4 commits por
detrás** del HEAD `b89c803`. El binario que corre en UAT no se instaló desde ese tag: lleva
empotrada la pseudo-versión del commit `b89c803`.

Este repo es **público** (`github.com/EduGoGroup/wapp-platform-console`): sin `GOPRIVATE` ni token.
Que sea público también es la razón de que su workflow de sincronización no consuma cuota de
Actions.

---

## 4 · 🔴 El hueco declarado: NADIE tiene un procedimiento propio para el primer administrador

**Esto es un hueco, no un olvido de esta documentación.** Sin una cuenta con acceso a
`wapp.platform`, **la consola no sirve para nada**: `/login` devuelve 401 y no hay forma de entrar.

Lo que se sabe, verificado:

1. **No hay ningún comando en este repo que cree un operador.** El `Makefile` tiene cinco targets y
   ninguno toca identidades.
2. **El bootstrap del ecosistema (`make grant-admin`, `make issue-api-key`) solo corre contra base
   local** y está bloqueado a `localhost` **sin variable de escape** — y eso «es la decisión, no un
   olvido». No alcanza a un ambiente remoto.
3. **En la práctica se hizo dos veces con `psql` crudo, fuera del `Makefile`, y ningún documento
   conservó el comando.** Cuando se midió, `iam.api_keys` estaba **vacía** y había **7 usuarios con
   0 asignaciones de rol**: ningún administrador en toda la base.
4. Existe hoy un runbook en el **repo de documentación del ecosistema**
   (`docs/runbooks/alta-staff-plataforma.md`), pero **no está en este repo**: quien clone solo esta
   pieza no lo tiene. Y **arrastra dos afirmaciones caducas** (ver abajo).

### El procedimiento, en corto, para que no se pierda otra vez

Son **cuatro escrituras en DOS bases distintas**. No hay endpoint que las haga.

| # | Base | Qué |
|---|---|---|
| 1 | **identity-core** | `INSERT` en `iam.users`: id nuevo, correo propio del staff, `password_hash` **bcrypt coste 12** (`password_hash` es `NOT NULL` sin default: va en el mismo INSERT). |
| 2 | **identity-core** | `INSERT` en `iam.user_systems` resolviendo el `system_id` **por `key`** (los ids nacen con `gen_random_uuid()`; nunca los *hardcodees*). |
| 3 | **wApp** | `INSERT` en `public.tenant_members` con el tenant de plataforma, id fijo `55550000-0000-0000-0000-000000000055` (slug `wapp-platform`). |
| 4 | **wApp** | `INSERT` en `public.iam_user_roles` con el rol `platform_admin`, id fijo `10000000-0000-0000-0000-000000000004`. |

🔴 **Corrección al runbook del ecosistema, medida el 2026-08-30:** el runbook manda abrir el
System Gate para **`wapp.bff`**, porque cuando se escribió el canje solo aceptaba `wapp.bff` y
`wapp.edge`. **Hoy el canje acepta también `wapp.platform`**, y esta consola se presenta
**exclusivamente** como `wapp.platform`. ⇒ **Para entrar aquí, el paso 2 tiene que ser
`key = 'wapp.platform'`**. Con solo `wapp.bff` el login falla con 403 del System Gate, y el log de
la consola lo dice con esas palabras.

🔴 **Segunda corrección:** el runbook habla de Neon como base de UAT. **Ya no**: la base de UAT es
un **PostgreSQL 17 en Docker** en el propio VPS. Las advertencias sobre el host `-pooler` no
aplican allí.

⚠️ **Antes de teclear nada, dos avisos que sí siguen vigentes:**
- **La cuenta de staff tiene que ser NUEVA y propia.** El canje exige **exactamente una** fila en
  `tenant_members` por usuario: añadir la membresía de plataforma a una cuenta que ya es de un
  cliente **le rompe el login entero**, no solo el permiso nuevo.
- **identity es COMPARTIDA y vive en producción.** No hay una identity de UAT y otra de
  producción. Esa cuenta nace con capacidad de cortar empresas y vale igual en los dos sitios.
  Y **identity no tiene reset de contraseña**: si se pierde, el único camino es un `UPDATE` a mano.

**Que esto no tenga un comando con registro es deuda con dueño** — está en
[`deuda.md`](deuda.md). El acto más sensible del sistema sigue siendo el único sin traza.

---

## 5 · Cómo se depura cuando falla

### La regla de oro: el binario instalado no es el proceso vivo

Instalar y reiniciar son **dos pasos**. Pregunta por el proceso, no por el fichero:

```bash
readlink /proc/$(systemctl show -p MainPID --value wapp-platform-console)/exe
md5sum   /proc/$(systemctl show -p MainPID --value wapp-platform-console)/exe
go version -m /usr/local/bin/wapp-platform-console | grep vcs.revision
```

El `vcs.revision` del buildinfo es la forma fiable de saber **qué commit corre**: ningún binario del
ecosistema responde a `-version`.

### Dónde está el log

En UAT, **no pasa por journald**: la unidad escribe a fichero. El log de esta consola es
`platform-console.log`, en **JSON** (porque el ambiente no es `local`). Una línea de arranque
sana se ve así:

```
{"time":"…","level":"INFO","msg":"consola de plataforma escuchando",
 "addr":"127.0.0.1:8106","admin_api":"http://127.0.0.1:8100","ambiente":"uat"}
```

Si el `ambiente` no es el que esperas, mira el `EnvironmentFile` de la unidad **antes** que el
código: recuerda que 16 nombres `WAPP_CONSOLE_*` los comparte con la consola del cliente
(ver [`contratos.md`](contratos.md)).

### Síntomas y su causa más probable

| Síntoma | Mira esto primero |
|---|---|
| **«Credenciales inválidas o sin acceso»** en el login | El log distingue lo que la pantalla oculta: `403` = falta la fila en `iam.user_systems` para **`wapp.platform`** (§4); `401` = contraseña. Un 401 **lento** (segundos) suele significar «el usuario existe y la contraseña no cuela»; uno rápido, «no existe» — el hash solo se paga si el usuario existe. |
| **La portada dice «No se pudieron cargar las empresas»** | `:8100` no responde, o el token no tiene `tenants.read.any`. Ojo: esa pantalla devuelve **`200 OK`**, así que una sonda no la ve caída. |
| **Vuelves al listado sin ningún mensaje** al abrir una ficha | Falló `GET /admin/tenants/{id}`. Es la única ruta de error del repo **sin flash**: el motivo solo está en el log. |
| **Un `500` opaco al dar de alta una empresa** | El `plan_id` elegido no existe en `public.plans`: `tenants.plan_id` es FK y el mapeo de errores del cloud solo reconoce la violación de UNIQUE, no la de FK. |
| **El código de enrolamiento sale en blanco o no aparece** | El `Path` de la cookie efímera y el destino del 303 se construyen con **una sola función**; si alguien los separó, el navegador deja de mandar la cookie y **nada falla al compilar**. |
| **El TTL del código no es el que pediste** | La clave del JSON es `ttl`, no `ttl_seconds`; una clave desconocida se ignora **sin error**. |
| **429 en el health check** | `/healthz` está detrás del rate-limit. |
| **`panic` al arrancar** | Solo hay tres causas: `TrustedProxies` con formato inválido, plantillas que no compilan, u opciones inválidas del cliente de identidad. Las tres fallan **en el arranque a propósito**, no dentro de un login. |
| **La página se ve sin estilo / con texto invisible** | Falta el `Nonce` en ese `c.HTML`, o alguien tocó el bloque monotema de `internal/web/static/css/app.css`. Corre `make test`: dos tests lo vigilan. |
| **Un formulario devuelve 403 al enviarlo** | Falta el `CSRFToken` en ese `c.HTML`: rompe **en runtime**, sin error de compilación. |

### Acceso a la consola en UAT

Escucha en **loopback** y **no se expone a Internet**: se llega por **túnel SSH**. Eso es
deliberado y es la mitigación que hace tolerable INV-PC1 (la consola no autentica a nadie por sí
misma). **No la publiques.**
