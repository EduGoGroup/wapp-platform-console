# Constitución de `wapp-platform-console`

Las reglas de esta pieza. Si una propuesta choca con algo de aquí, **la propuesta está mal**, no
la regla. Este repo se clona solo: lo esencial del ecosistema está **repetido** abajo, no
enlazado, porque un enlace a otro repo no resolvería.

---

## 0 · Qué es esta pieza y qué NO es

Es la consola web de **operadores de plataforma** (nosotros, el staff de wApp). Escucha en
`127.0.0.1:8106`. **No** es la consola del cliente (`wapp-client-console`, `:8107`), **no** es el
BFF del cliente (`wapp-guardian-bff`, `:8104`) y **no** es el plano de control del Edge
(`:8105`). Ver la tabla comparativa en [`README.md`](README.md).

Hace esto y nada más: alta de empresas, códigos de enrolamiento de Edge, corte y restauración del
servicio de una empresa (kill-switch **comercial**) y bandeja de solicitudes de acceso.

---

## 1 · Los invariantes del ECOSISTEMA que esta pieza puede romper

Estos no son suyos: son de todo wApp. Se repiten aquí porque **desde este repo se pueden violar**.

### INV-E1 · Zero-knowledge: la nube nunca ve credenciales ni llaves privadas

wApp es un ecosistema de mensajería sobre WhatsApp cuyo núcleo corre 24/7 **en el equipo del
cliente** (el Edge), gobernado por una plataforma cloud. El principio zero-knowledge protege
**credenciales y llaves**, **no** el contenido de negocio: los pedidos, contactos y mensajes sí
suben a la nube, a propósito.

**Qué significa aquí:** esta consola **jamás** debe pedir, mostrar, registrar ni transportar
material de llave. El único material sensible que toca es el **código de enrolamiento**, y por eso
va en una cookie efímera y **nunca en la URL** (ver INV-PC5).

**Cómo se comprueba:** `grep -rn "DEK\|privkey\|private_key\|KEK" --include='*.go' .` → cero
aciertos (2026-08-30). **Test que lo vigila:** ninguno; es regla de revisión.

### INV-E2 · Doble llave: la DEK es del cliente, el Lease es del servidor

- **DEK** — descifra el almacén de sesión de `whatsmeow` en el Edge. **La custodia el cliente y
  jamás cruza ningún contrato.** Ni la nube ni esta consola la tienen, la piden ni la ven.
- **Lease** — autoriza a un Edge a operar. **Lo emite y lo revoca el servidor**: es el kill-switch
  **anti-clon**, por instalación.

🔴 **No confundas los dos kill-switch, porque esta consola opera el otro.** Lo que hacen
`POST /tenants/:id/revoke` y `/restore` es el kill-switch **COMERCIAL**, por *tenant*
(«esta empresa dejó de pagar»): pone y quita `revoked_at` en la fila de la empresa. El
kill-switch **anti-clon** del lease es por instalación y vive en otra ruta del cloud
(`/admin/leases/revoke`), que **esta consola no llama**. La migración que introdujo el permiso
`.any` lo dice explícitamente: el deny `'*.any'` **no** cubre `leases.revoke`, y eso es
deliberado.

**Cómo se comprueba:** `grep -rn "lease" --include='*.go' internal/` → el único acierto es el
campo `LeaseRevoked` que la ficha de empresa **muestra** (`internal/adminclient/installations.go`),
nunca escribe.

### INV-E3 · Sin Redis ni broker en el Edge — y aquí, sin nada de infraestructura

La concurrencia del ecosistema se resuelve con Go, no con brokers. Aquí la regla es más fuerte:
**esta consola no tiene infraestructura de ningún tipo** — ni base de datos, ni cola, ni caché, ni
fichero de estado. Cero. **Cómo se comprueba:**
`grep -rn "database/sql\|pgx\|redis\|amqp" go.mod go.sum` → cero; no hay migraciones, ni DSN, ni
versión de esquema. 🔴 **No añadas una base de datos aquí**: si hay que persistir algo, el sitio es
el cloud y el camino es un endpoint nuevo en `:8100`.

### INV-E4 · Copia-adaptación, nunca dependencia: prohibido importar `edugo-*`

Parte del código del ecosistema se **copió y adaptó** de otro producto (EduGo) al espacio de
nombres de wApp. **Está prohibido importar un repo `edugo-*`.** Ojo con el falso amigo: la
organización de GitHub se llama `EduGoGroup`, así que los módulos legítimos
`github.com/EduGoGroup/wapp-shared/...` **sí** están permitidos — lo prohibido es un módulo cuyo
nombre empiece por `edugo-`.

**Cómo se comprueba:**
```bash
grep -n "EduGoGroup/edugo" go.mod go.sum   # debe dar CERO
```
Excepción real y única hoy: `github.com/EduGoGroup/identity-shared/auth v0.3.1`, que entra como
**indirecta** vía `wapp-shared/iam`. No es un repo `edugo-*` y no se importa directamente.

### INV-E5 · El código compartido interno vive en `wapp-shared`

`wapp-shared` es un monorepo **multi-módulo propio de wApp** con releases **por módulo**
(tags `<modulo>/vX.Y.Z`). Esta consola consume **cinco** de sus módulos, y los cinco están hoy en
su último tag publicado:

| Módulo | Versión | Qué aporta aquí |
|---|---|---|
| `wapp-shared/web` | `v0.2.0` | CSRF, cabeceras de seguridad + nonce, CORS, rate-limit, cookies, sesión, `FlashCatalog`, `RefreshGroup`, `OneTimeCookie` |
| `wapp-shared/iam` | `v0.1.0` | cliente de identity: login / refresh / logout + canje de token |
| `wapp-shared/ui` | `v0.4.1` | las tres hojas CSS compartidas, servidas por `ui.GetCSS` |
| `wapp-shared/auth` | `v0.5.0` | el tipo `sharedjwt.Claims` |
| `wapp-shared/config` | `v0.3.0` | lector de entorno con prefijo |

🔴 **Si vas a escribir aquí un middleware de seguridad, para.** Esta consola ya tuvo sus propias
copias de CSRF, cabeceras y cookies, y el Plan 047 · Ola 0.5 las **borró** (commit `58ef114`)
precisamente porque eran un tercer fork. Lo transversal sube a `wapp-shared/web`; aquí solo se
configuran sus opciones.

---

## 2 · La EXCEPCIÓN ADMINISTRATIVA A INV-8: por qué este código no es una violación

**Léelo antes de tocar `internal/adminclient/tenants.go`.** Sin esta explicación, el código de esta
consola parece exactamente lo que el resto del ecosistema tiene prohibido.

**INV-8**, la regla general de wApp: *el tenant sobre el que se actúa sale del token del llamante,
**jamás** del cuerpo de la petición*. Es lo que impide que el cliente A toque datos del cliente B.

**Esta pieza hace justo lo contrario, y está autorizado por el ADR-0039** («Plano de plataforma y
excepción administrativa a INV-8», en el repo de documentación del ecosistema). El motivo, medido
y no supuesto: antes del ADR, `/admin/tenants/revoke` tomaba el tenant objetivo de la identidad del
propio llamante, así que **wApp no podía cortar a un cliente moroso** — solo el cliente podía
cortarse a sí mismo, y se desrevocaba solo llamando a `/restore`. El kill-switch comercial existía
en la tabla y no existía en la práctica.

**El ADR separa dos cosas que INV-8 tenía fundidas:**
- el tenant que **EJECUTA** → sigue saliendo del token, jamás del cuerpo;
- el tenant **OBJETIVO** → viaja en el cuerpo.

Eso se ve literalmente en este repo: `RevokeTenant` y `RestoreTenant` mandan
`{"tenant_id": ..., "reason": ...}` **en el cuerpo** a `POST /admin/tenants/revoke`
(`internal/adminclient/tenants.go`), mientras el `Authorization: Bearer` lleva el token del
operador. Eso es la excepción, escrita.

**Las tres cercas que impiden que la excepción sea un agujero** (viven en el cloud, no aquí, pero
tienes que conocerlas para no proponer nada que las salte):

1. **Permisos con sufijo `.any`.** Toda ruta del plano de plataforma exige un permiso terminado en
   `.any` — `tenants.read.any`, `tenants.create.any`, `tenants.revoke.any`, `tenants.restore.any`,
   `fleet.read.any`, `users.provision.any`, `enrollment.issue.any`. El sufijo nombra exactamente
   lo que se concede: **actuar sobre un tenant que no es el tuyo**.
2. **Un `deny '*.any'` sobre `tenant_admin`.** Sin él las otras dos piezas no valdrían nada:
   `tenant_admin` tiene el grant `*`, y `*` casa con cualquier permiso, así que todo administrador
   de todo cliente ya tendría `tenants.revoke.any`. El deny lo tapa **por forma**, cubriendo
   cualquier permiso `.any` futuro sin depender de que nadie se acuerde de añadirlo a una lista.
3. **Comprobación de pertenencia en el propio handler del cloud**: además del RBAC, cada handler
   del plano exige que el token sea del **tenant de plataforma** (el id fijo
   `55550000-0000-0000-0000-000000000055`, slug `wapp-platform`).

**Consecuencia para esta consola:** el rol `platform_admin` **NO se ofrece** en el desplegable de
la bandeja de acceso. Ofrecerlo convertiría el autoservicio de solicitudes en una vía de escalada
de privilegios. Está razonado en la propia plantilla
(`internal/web/templates/pages/access_requests.html`) y **atado por test**:
`TestAccessRequests_RoleOptionsExistenEnElCatalogo` (`internal/web/catalog_test.go`) comprueba que
los roles ofrecidos son un subconjunto estricto de los sembrados, con `platform_admin` fuera.
Su hermano `TestAccessRequests_NoPlatformCheckbox` hace lo mismo con la casilla `wapp.platform`.

**Veredicto de la verificación del 2026-08-30:** el ADR-0039 **CUMPLE**, con sus tres cercas y con
un gate por AST en el cloud (`TestINV056_1_PlatformPermissionsMustEndInDotAny`, que además tiene
**caso negativo**: un test que demuestra que el detector detecta).

---

## 3 · Los invariantes PROPIOS de esta pieza

### INV-PC1 · Esta consola NO es un perímetro de seguridad. El gate real es `:8100`

🔴 **El invariante más importante del repo, y el más fácil de leer al revés.**

La cookie de sesión es `base64(JSON)` **sin firmar ni cifrar**, y el JWT que lleva dentro se lee
con `ParseUnverified` — la firma **nunca se verifica aquí** (`internal/web/session.go`, función
`parseAccessClaims`, con el porqué escrito encima). El `AuthMiddleware`
(`internal/web/auth_handler.go`) solo comprueba que haya un `exp` en el futuro.

**Consecuencia real:** cualquiera que pueda hablar con `:8106` puede fabricar una cookie a mano y
navegar todas las pantallas «protegidas». La prueba está en la propia suite: el helper
`makeAdminToken` de `internal/web/server_test.go` firma con la clave literal `"dummy"` y la consola
lo acepta.

**Por qué no se filtra nada:** cada llamada a `:8100` lleva ese mismo token como
`Authorization: Bearer`, y **el cloud sí lo valida** (firma, RBAC `.any` y pertenencia al tenant de
plataforma). Un token falso navega pantallas vacías y recibe 401/403 en cuanto pide un dato.
Mitigante adicional: escucha en **loopback**.

**Qué NO puedes hacer con esto:**
- **No** añadas aquí una decisión de autorización («si el rol es X, enseña el botón Y») creyendo
  que autoriza algo: no autoriza nada, es cosmética. La autorización se pide a `:8100`.
- **No** «arregles» esto poniendo aquí una verificación de firma con una clave copiada: eso
  duplicaría el emisor y crearía un segundo perímetro que se desincronizaría.
- **No** expongas este puerto a Internet.

**Cómo se comprueba:** `grep -n "ParseUnverified" internal/web/session.go` → una aparición, con
su comentario. `grep -n "hmac\|HMAC\|Sign" internal/web/session.go` → **cero**.
**Test que lo vigila:** ninguno. Es deuda declarada (**M-09** del code review del Plan 056, abierta;
ver [`deuda.md`](deuda.md)).

### INV-PC2 · Los nombres de cookie son de ESTA consola y no se comparten

`wapp_platform_session`, `wapp_platform_csrf`, `wapp_platform_enrollment_code`
(`internal/web/session.go`). `wapp-shared/web` los expone como **parámetro**, no como constante,
justo porque el BFF del cliente y esta consola conviven y una constante compartida las haría
pisarse la cookie.
**Test que lo vigila:** ✅ `TestCookieNames_SonLasDeEstaConsolaYNoLasDelBFF`
(`internal/web/cookies_test.go`).

### INV-PC3 · El `system` ante identity es `wapp.platform`, y son dos perímetros distintos

Constante `systemWappPlatform` en `internal/web/server.go`. El BFF del cliente se presenta con
`wapp.bff`. **Compartir el cliente de identidad no acerca los dos perímetros ni un milímetro.**
Consecuencia operativa: un operador sin fila en `iam.user_systems` para `wapp.platform` **no puede
entrar aquí** aunque entre en el BFF (ver [`operacion.md`](operacion.md)).

### INV-PC4 · La barrera del corte se resuelve en el SERVIDOR, nunca en un campo oculto

`DoRevokeTenant` (`internal/web/tenants_handler.go`) pide el slug de la empresa **escrito a mano**
y lo compara contra el slug real que resuelve él mismo llamando a `GetTenant` con el `id` de la
URL. **El slug esperado nunca viaja en el formulario**: un campo oculto es indistinguible de un
valor de ataque. Un `slug_confirm` que coincida con el slug de OTRA empresa no basta.
**Tests que lo vigilan:** ✅ `TestTenants_RevokeRequiresSlugConfirmation`,
`TestTenants_RevokeUnreachableTenantDoesNotCallRevoke` y
`TestTenants_RevokeAndSlugMismatchAreVisibleToOperator` (`internal/web/tenants_test.go`).
⚠️ La plantilla imprime hoy el slug esperado encima del input y como `placeholder`: es fricción,
no confirmación fuera de banda. Ver [`deuda.md`](deuda.md).

### INV-PC5 · El código de enrolamiento NO viaja por la URL, y es de un solo uso

El código autoriza a enrolar un Edge durante 24 h. Una URL acaba en el log del proxy, en el
`Referer` y en el historial. Por eso:
- `POST /tenants/:id/enrollment-codes` responde **303** y deja el código en la cookie efímera
  `wapp_platform_enrollment_code`: 60 s de vida y `Path` acotado **a la pantalla exacta** del
  tenant (`enrollmentCodePath`, en `internal/web/provisioning_handler.go`).
- `GET /tenants/:id/enrollment-code` **lee y borra la cookie en el mismo gesto**. Un F5 no
  encuentra nada y redirige al detalle **sin emitir un código nuevo**.

🔴 El redirect y el `Path` de la cookie se construyen con **una sola función**, `enrollmentCodePath`.
Si los separas, basta tocar uno para que el navegador deje de mandar la cookie y **nada falla al
compilar**: la página sale vacía y solo se ve en producción.
**Tests que lo vigilan:** ✅ `TestEnrollmentCode_F5NoReemite`,
`TestEnrollmentCode_SinCookieNoHayPantalla`, `TestEnrollmentCode_LaCookieEsHttpOnlyYAcotada`
(`internal/web/enrollment_code_test.go`).

### INV-PC6 · El texto de un flash sale del catálogo, nunca del query string ni del upstream

Los handlers redirigen con **códigos estables** (`?error=slug_mismatch`), no con texto. El texto lo
pone `internal/web/flash.go` mediante `FlashCatalog` de `wapp-shared/web`; un código desconocido
cae al genérico. Reflejar el mensaje crudo del upstream rompería la query con un `&` o un `#` y
abriría una superficie de reflejo gratuita (**M-11**).
**Tests que lo vigilan:** ✅ `TestFlashError_UnknownCodeNeverReflectsRawInput` y los dos de códigos
conocidos (`internal/web/flash_test.go`).

### INV-PC7 · Aprobar una solicitud NUNCA inventa un default que conceda sistemas

`PUT /users/{id}/systems` del lado de identity es **declarativo**: manda la lista completa y
reemplaza. Por eso `DoApproveAccessRequest` (`internal/web/access_requests_handler.go`) **rechaza**
con `missing_systems` si no hay ninguna casilla marcada, en vez de rellenar un default. Y por eso
la bandeja **precarga** las casillas con los sistemas que el usuario **ya tiene**
(`AccessRequestItem.Systems`), distinguiendo con `SystemsKnown` entre «no tiene ninguno» y «el
servidor no pudo leer su estado»: en el segundo caso la pantalla **avisa** en vez de marcar
casillas «por si acaso».
**Tests que lo vigilan:** ✅ `TestAccessRequests_ApproveRejectsWhenNoSystemsSelected`,
`TestAccessRequests_PrecargaFromCurrentSystems`,
`TestAccessRequests_UnknownSystemsWarnsAndLeavesUnchecked` (`internal/web/access_requests_test.go`).

### INV-PC8 · Esta consola es MONOTEMA CLARA, y es una decisión medida

`internal/web/static/css/app.css` está escrita con literales índigo/pizarra y **ningún valor de
tema**. Las hojas compartidas que se cargan antes **sí** traen tema, así que `app.css` **redeclara
los tokens oscuros con su valor claro dentro de la misma media query y con el mismo selector**.
El motivo está medido: el índigo de marca `#4338CA` da 7,90:1 sobre la tarjeta clara y **2,17:1**
sobre la oscura; seguir el tema sería un cambio de marca, no una corrección de contraste.

🔴 **El par color/fondo tiene que viajar entero**: o los dos literales, o los dos tokenizados,
nunca uno de cada. Un literal fijo pintado sobre una superficie que se mueve con el tema es el
defecto que este bloque cierra (llegó a medirse 1,00:1 — texto invisible — en `tenant_detail`).
**Tests que lo vigilan:** ✅ `TestCSS_TokensSensiblesAlTemaNeutralizados` y
`TestCSS_ColorSchemeDeclaradoLight` (`internal/web/theme_test.go`). El primero **deriva la lista de
tokens del CSS del módulo pinado** en vez de copiarla, para que un release nuevo de
`wapp-shared/ui` lo ponga en rojo en vez de envejecer en silencio. **Es el ejemplo a imitar.**

### INV-PC9 · Cero JavaScript, cero inline styles

No hay un solo `<script>` en el repo ni htmx: cada acción es un `<form method="POST">` con
redirect. La CSP no admite `style=` inline.
**Test que lo vigila:** ✅ `TestTemplates_NoInlineStyles` (`internal/web/security_test.go`), sobre
las 7 páginas.

---

## 4 · 🔴 AVISO PERMANENTE sobre la superficie de acceso y sistemas

**Esto pasó de verdad, y por eso está en la constitución y no en un journal.**

El code review del Plan 056 (2026-08-15) encontró que **el signup público y sin autenticar**
—la puerta por la que una persona pide acceso, la misma que alimenta la bandeja de esta consola—
caía a `EnsureUser` ante el 409 de identity y llamaba a `ReplaceUserSystems`, que es
**declarativo**, con **un solo elemento**. Traducido: **bloqueo remoto de cuentas ajenas conociendo
solo el correo, repetible a discreción**. Y lo peor: **el test consagraba el agujero** — exigía
`ensureCalled == true`, es decir, comprobaba que se hiciera exactamente lo dañino.

Se corrigió. Lo que queda es la lección, que aplica cada vez que alguien toque esta superficie:

1. **Toda escritura de sistemas o roles es declarativa hasta que se demuestre lo contrario.**
   Mandar «lo que quiero conceder» **borra lo que no mandas**. De ahí INV-PC7.
2. **Una ruta pública y sin autenticar que acaba escribiendo permisos es material crítico**, aunque
   el fichero se llame `signup` y parezca inofensivo.
3. **Un test verde puede estar consagrando el defecto.** Ante un aserto sobre una llamada
   sensible, pregunta si comprueba que ocurra o que **no** ocurra — y mútalo para verlo.
4. En la misma revisión salió **C-02**: un puntero nil dentro de una interfaz hacía que las tres
   guardas `== nil` fueran código muerto y la ruta pública **entrara en pánico**. Un `!= nil` sobre
   una interfaz que envuelve un puntero nil es verdadero.

Para calibrar cuánto fiarse de un `tasks.md`: aquel veredicto fue *«de 23 tareas marcadas `[x]`,
10 están completas, 12 parciales y 1 está funcionalmente rota»*, y el propio informe declaraba que
**en ese entorno no había Go: nada se compiló, vetó, linteó ni ejecutó**.

---

## 5 · Tecnología y versiones reales

Sacadas de `go.mod` (verificado el 2026-08-30):

- **Go `1.26.5`** — línea 3 de `go.mod`. El `Makefile` fija `GO_VERSION := 1.26.5` y el CI el mismo.
- **`github.com/gin-gonic/gin v1.10.0`** — el router y todo el HTTP.
- **`github.com/golang-jwt/jwt/v5 v5.3.1`** — leer claims **sin verificar firma** (INV-PC1).
- Los cinco módulos de `wapp-shared` de la tabla de INV-E5.
- Indirecta relevante: `github.com/EduGoGroup/identity-shared/auth v0.3.1`, vía `wapp-shared/iam`.
- **golangci-lint `v2.12.2`**, fijado en el `Makefile`. Linters activos (`.golangci.yml`):
  `errcheck` (con `check-type-assertions`), `govet`, `ineffassign`, `staticcheck`, `unused`,
  `errorlint`, `errname`, `nilerr`; formateadores `gofmt` y `goimports`.
- **Base de datos: ninguna.** **Broker: ninguno.** **Frontend: `html/template` embebido.**

---

## 6 · Convenciones de código

- **Todo en `GOWORK=off`.** Los cinco targets del `Makefile` lo llevan. Si compilas o testeas a
  mano, ponlo tú: sin él arrastras el `go.work` del árbol del ecosistema y dejas de probar las
  versiones **publicadas** de `wapp-shared` que este repo declara.
- **Español en comentarios, nombres de test y textos de UI**, con identificadores Go en inglés
  donde toca. Los tests nuevos siguen el estilo existente: `TestCSS_ColorSchemeDeclaradoLight`.
- **Los comentarios explican POR QUÉ, y son largos a propósito**: hasta esta documentación, eran
  la única que tenía el repo. No los podes al «limpiar»: varios registran un defecto ya pagado.
- **Cero `TODO`/`FIXME`/`HACK` en todo el repo** (verificado). La deuda se escribe con su razón y
  su dueño, no con una etiqueta. Mantenlo así: si vas a dejar deuda, va a
  [`deuda.md`](deuda.md).
- **Errores:** sentinelas y tipos con `errors.Is`/`errors.As` (`ErrUnauthorized`, `APIError`,
  `RejectionError`, `PartialApprovalError` en `internal/adminclient/transport.go`). `errorlint` y
  `errname` están activos: no compares errores con `==` ni nombres un tipo de error sin sufijo
  `Error`.
- **Un `_ =` sobre un error necesita comentario** que diga por qué se descarta. Los legítimos hoy
  son `drainClose` y el decode acotado del cuerpo de rechazo.
- **Todo cuerpo de respuesta se lee acotado**: `io.LimitReader` con `maxSuccessBody` (1 MiB) o
  `maxRejectionBody` (4 KiB). No añadas un `json.Decode` sin límite.

---

## 7 · Trampas conocidas — lo que un agente hace mal aquí si nadie se lo dice

**T1 · Creerte que estás en la consola del cliente.** Es el error nº 1: si la pantalla que buscas
habla de pedidos, catálogo, flujos o sesiones de WhatsApp, es `wapp-client-console`.

**T2 · Escribir aquí un middleware de seguridad.** Sus copias se borraron en el Plan 047 · Ola 0.5;
lo transversal va a `wapp-shared/web`.

**T3 · Leer el `AuthMiddleware` como si autorizara.** No autoriza (INV-PC1): no cuelgues de él
ninguna decisión que importe.

**T4 · La clave del TTL es `ttl`, NO `ttl_seconds`.** El servidor la declara así
(`platformadmin/handlers.go` del cloud, `TTLSeconds int json:"ttl,omitempty"`). `encoding/json`
ignora claves desconocidas **sin error**, así que un desajuste hace que el TTL elegido no llegue
nunca y el código viva siempre el default del servidor, **sin ningún aviso**.
✅ Lo blinda `TestTenants_IssueEnrollmentCode_SendsTTLKeyNotTTLSeconds`
(`internal/web/tenants_test.go`).

**T5 · El `<select>` de planes está DESACTUALIZADO y su test no puede verlo.** La plantilla, el
parcial `plan_label.html` y `catalog_test.go` afirman que `public.plans` tiene **cinco** planes.
**Son seis desde el 2026-08-23**: la migración `0074_seed_plan_advisor_ai_local.sql` del cloud
siembra `advisor_ai_local`. El candado de `catalog_test.go` es **de un solo sentido** —comprueba
`ofrecido ⊆ sembrado`, nunca `sembrado ⊆ ofrecido`—, así que un plan nuevo en el cloud
**envejece en silencio**. Consecuencia hoy: **desde esta consola no se puede dar de alta una
empresa con el plan `advisor_ai_local`**. Ver [`deuda.md`](deuda.md).

**T6 · «Opera los planes» es falso.** El `plan_id` **solo** se puede fijar **en el alta**. **No
existe ninguna ruta para cambiar el plan de una empresa** ni aquí (las 19 rutas) ni en el cloud
(`registerAdminRoutes`). Cambiar de plan sigue siendo un `UPDATE` a mano — justo el escenario que
el Plan 056 vino a eliminar.

**T7 · El `.env` NO lo lee nadie.** `.env.example` está escrito con esmero y `.gitignore` reserva
`.env`, pero el lector de `wapp-shared/config` solo consulta `os.LookupEnv` y **no hay `godotenv`
en `go.sum`**. Copiar `.env.example` a `.env` y hacer `make run` arranca con **todos los
defaults**, en silencio.

**T8 · Nada de la configuración es obligatorio.** Todas las variables tienen default, así que un
despliegue mal configurado **arranca igual** apuntando a `127.0.0.1`. El único fallo duro del
arranque es un `TrustedProxies` con formato inválido, que hace `panic`
(`internal/web/server.go`; ✅ `TestSetTrustedProxies_PanicsOnInvalidFormat`).

**T9 · 16 nombres `WAPP_CONSOLE_*` los COMPARTE con `wapp-client-console`, y las dos corren en la
misma máquina.** Un `WAPP_CONSOLE_ENV` o un `WAPP_CONSOLE_COOKIE_SECURE` exportado en el entorno
del host **se aplica a las dos a la vez**. La única forma de darles valores distintos son ficheros
de entorno separados — que es lo que hace UAT hoy. Detalle y lista completa en
[`contratos.md`](contratos.md).

**T10 · Cada `c.HTML` copia a mano las mismas 6-8 claves y no hay helper.** Olvidar `Nonce` deja la
página sin estilo permitido; olvidar `CSRFToken` **rompe el formulario en runtime, sin que compile
mal**. Hoy los 10 `c.HTML` están completos. Si añades el 11.º, cópialo entero.

**T11 · Un PR no valida nada.** `ci.yml` es `workflow_dispatch`. El gate real es local. Y `rc=0`
cuenta igual un `--- SKIP` que un `--- PASS`. Ver [`operacion.md`](operacion.md).

**T12 · No confundas los dos kill-switch** (comercial por tenant vs. anti-clon por lease). Ver
INV-E2.
