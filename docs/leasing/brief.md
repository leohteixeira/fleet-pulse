# Project Brief: Fleet Pulse, fase 2, Leasing e Persistência

Canonical contract: `_bmad-output/specs/spec-leasing/`. This brief stays the narrative
source; numbers and operator-visible rules below match that spec.

## Resumo
Evolução do Fleet Pulse. A fase 1 entregou a tela Frota, com carros de
aluguel, broker MQTT embutido, simulador, SSE e comando de destravamento com
máquina de estados, tudo em memória. A fase 2 adiciona uma frota separada de
carros financiados por leasing, uma tela de gestão de carteira com bloqueio
remoto seguro, persistência em PostgreSQL, simulação procedural para operação
contínua e deploy público em VPS.

A aplicação é pública e sem login. Qualquer visitante pode executar todas as
ações. A integridade da demo é garantida pelo próprio sistema, não por
autenticação.

Inspiração: modelo de leasing em que cada carro financiado tem um dispositivo
de telemetria com bloqueio remoto, reduzindo o risco de recuperação do ativo,
e oferta da mesma tecnologia a bancos como SaaS. Este envio continua sendo
um artefato de portfólio, não esse produto.

## Problema
1. Bloquear um veículo remotamente tem risco físico e jurídico. Nunca pode
   ocorrer com o carro em movimento, exige política, notificação prévia e
   trilha de auditoria.
2. Um desbloqueio perdido por dispositivo offline deixa um cliente adimplente
   sem o carro.
3. Uma demo pública rodando continuamente não pode se repetir de forma
   perceptível, não pode crescer sem limite e precisa se recuperar sozinha
   quando visitantes executam ações em massa.

## Frotas
1. Aluguel (fase 1): cerca de 20 veículos, viagens aleatórias sobre a
   biblioteca de rotas. Também passa a persistir no PostgreSQL. A máquina
   de destravar/travar de 5s não muda.
2. Leasing (fase 2): cerca de 50 veículos, cada um vinculado a um contrato
   e a um cliente com rotina própria. Tópicos `leasing/{vin}/telemetry`,
   `leasing/{vin}/commands`, `leasing/{vin}/ack`.
As frotas nunca se misturam em telas, tópicos lógicos ou consultas.

## Simulação procedural
1. Script offline gera biblioteca de rotas reais em São Paulo a partir de
   cerca de 60 pontos de interesse, usando OSRM uma única vez. Resultado
   versionado como seed, cerca de 300 rotas. Produção nunca chama serviço
   externo de rotas.
2. Aluguel: viagens entre pontos aleatórios com permanência variável.
3. Leasing: rotina por cliente (casa, trabalho, eventos de fim de semana)
   com variação aleatória de horários.
4. Ruído em velocidade, intervalo de publicação e bateria.
5. Agendador de incidentes com taxas aleatórias: dispositivo offline
   temporário, bateria baixa, perda de sinal.
6. Seed do gerador aleatório configurável para reproduzir cenários em
   desenvolvimento.
7. Dispositivos de leasing alcançáveis recusam cerca de 15% das execuções
   de bloqueio, além dos incidentes.

## Calendário simulado
1. Taxa só via ambiente: `1h/s`, `4h/s` (padrão, ×14400) ou `12h/s`. A UI
   mostra o badge e não expõe controle. O relógio de telemetria continua
   em tempo real. (`SIM_DAY_SECONDS=300` foi superado.)
2. Data simulada em `GET /api/clock` e no header da Carteira.
3. Perfis de pagador com pesos 40% pontual, 30% atrasa ocasionalmente,
   20% regulariza após notificação, 10% inadimplente.
4. Pagamentos gerados pelo perfil a cada vencimento, inclusive depois de
   intervenção de visitantes (autocura).
5. Ciclo de vida: contratos terminam, novos são criados, carteira mantida
   entre 40 e 60 contratos ativos. Piso de 10% em cada faixa de atraso.

## Leasing
1. Contrato: cliente, veículo, valor da parcela, total de parcelas, parcelas
   pagas, perfil de pagador, datas.
2. Faixas de atraso: em dia, 1 a 15, 16 a 30, acima de 30 dias simulados.
3. Ações: notificar cliente, solicitar bloqueio com motivo obrigatório
   (8–280 caracteres), cancelar bloqueio, registrar pagamento manual.
4. Política validada no servidor: mínimo de 16 dias em atraso, notificação
   prévia com pelo menos 48h (2 dias simulados), dispositivo online.
   Notificar exige atraso ≥ 1 dia e intervalo de 24h simuladas desde a
   última notificação. Pagamento manual só com parcela vencida ou a
   vencer em 5 dias simulados. Violação retorna 422 com código de erro
   legível e o copy do design.
5. Máquina de estados do bloqueio: REQUESTED, ARMED, SENT, ACKED,
   CANCELLED, FAILED, TIMEOUT. Rótulos na UI: SOLICITADO, ARMADO,
   ENVIADO, CONFIRMADO, CANCELADO, FALHOU, EXPIRADO. ARMED só avança
   para SENT com velocidade zero, ignição desligada e dispositivo online.
   TIMEOUT é ARMED com dispositivo offline por mais de 30s (âmbar, não
   vermelho). Não usa a janela de 5s da frota de aluguel.
6. Pagamento que zera o atraso cancela bloqueio em REQUESTED ou ARMED e
   emite desbloqueio para veículo bloqueado.
7. Desbloqueio com dispositivo offline fica pendente e é reentregue na
   reconexão.
8. Veículo bloqueado não liga a ignição nem se move no simulador.
9. Auditoria append only: timestamp real, data simulada, origem (visitante
   ou sistema), ação, motivo, estado anterior, estado novo, correlation id.
   Para visitantes, registrar apenas um identificador anônimo derivado do IP
   com hash, nunca o IP em claro.

## Proteção da demo pública
1. Sem login. Todas as ações disponíveis para qualquer visitante.
2. Rate limit por IP nas rotas de escrita: 6 escritas / 60s, resposta 429
   e header Retry-After.
3. Intervalo mínimo de 20s entre ações no mesmo contrato, independente do
   visitante, resposta 409 e código de erro legível.
4. Idempotency-Key obrigatória nas rotas de escrita, escopo contrato + ação
   (notify | block | cancel | payment).
5. Autocura: o sistema regulariza contratos segundo o perfil de pagador
   mesmo após intervenção de visitantes, e nenhum veículo permanece
   bloqueado por mais de 30 dias simulados.
6. Piso de 10% de contratos em cada faixa de atraso.
7. No máximo 64 conexões SSE simultâneas.
8. Broker MQTT acessível apenas na rede interna.
9. Headers de segurança. CORS same-origin: Caddy serve UI e API no mesmo
   host.
10. Body JSON no máximo 8 KiB. Motivo de bloqueio 8–280 caracteres.

## Persistência
1. PostgreSQL, driver pgx, queries com sqlc, migrações com goose.
2. Tabelas: vehicles, vehicle_state, routes, customers, contracts,
   installments, payments, commands, audit_log, outbox.
3. vehicle_state com upsert e cache em memória na frente.
4. Histórico de posições apenas para leasing, amostrado por minuto,
   retenção de 7 dias por job de limpeza. Item opcional, primeiro corte.
5. Retenção de auditoria e comandos finalizados limitada por job de limpeza.
6. Transactional outbox: comando, auditoria e mensagem de outbox gravados na
   mesma transação. Relay publica no MQTT e marca como enviado.
7. Store em memória da fase 1 substituído mantendo a mesma interface.

## API
1. GET /api/clock
2. GET /api/leasing/vehicles
3. GET /api/contracts com filtros por faixa e estado
4. GET /api/contracts/{id} com parcelas e auditoria
5. POST /api/contracts/{id}/notify
6. POST /api/contracts/{id}/block com motivo e Idempotency-Key, responde 202
7. POST /api/contracts/{id}/block/cancel
8. POST /api/contracts/{id}/payments
9. GET /api/stream?fleet=rental|leasing — um EventSource; o servidor
   envia só aquela frota. Query obrigatória; valor inválido retorna 400.
   Trocar de aba reabre a conexão e não é STREAM CAIU.
   A Frota da fase 1 passa a chamar ?fleet=rental.
Atualizar openapi.yaml.

## Frontend
Abas Frota e Carteira no header, com react-router-dom por baixo:
`/` Frota, `/carteira` Carteira, `/carteira/:id` contrato selecionado.
Id desconhecido mantém a Carteira com o painel vazio, sem toast.
Carteira com mapa dos carros financiados (viewport Grande SP do design),
tabela de contratos, indicadores, filtros, painel de detalhe com parcelas
e auditoria, diálogo de confirmação de bloqueio. Data simulada no header
(badge de velocidade, sem controle). Tratamento de 409, 422 e 429 com as
mensagens do design. Contrato visual em `docs/leasing/design/` e
`_bmad-output/specs/spec-leasing/ux.md`. Share do mapa é 42%.

## Deploy
1. Docker compose com app, PostgreSQL e Caddy com HTTPS automático.
2. Binário único com frontend embutido. Fallback de `index.html` para o SPA.
3. Healthcheck da app e do banco.
4. Migrações executadas na inicialização.
5. Logs estruturados em JSON.

## Critérios de aceite
1. Após uma hora rodando, trajetos e eventos não se repetem de forma
   perceptível.
2. Contratos entram e saem da inadimplência sozinhos.
3. Bloqueio sem notificação prévia, com menos de 16 dias, com notificação
   há menos de 48h ou com dispositivo offline é recusado com mensagem clara.
4. Bloqueio solicitado com carro em movimento fica ARMED até o carro parar.
5. Carro bloqueado para no mapa da Carteira.
6. Pagamento libera o carro, inclusive após reconexão de dispositivo offline.
7. Um visitante disparando ações em sequência recebe 429 e a demo se
   recupera sozinha em poucos minutos (30 dias simulados ≈ 3 min a 4h/s).
8. Reiniciar o container preserva contratos, comandos e auditoria.
9. Uso de disco estável ao longo de dias.

## Fora de escopo
Autenticação, juros e multa, boletos, notificação real ao cliente, aprovação
em duas etapas, multi tenant, testes end to end, alta disponibilidade,
controle de velocidade do calendário na UI, histórico de posições se a
janela apertar.

## Pós fase 2
Autenticação de operador com OAuth 2.0, aprovação em duas etapas para
bloqueio, métricas de tempo entre solicitação e efetivação do bloqueio,
PostGIS para consultas geoespaciais, fan out de eventos via NATS para
múltiplas réplicas.

## Riscos
1. Escopo excede a janela disponível. Prioridade: persistência e outbox,
   máquina de bloqueio, simulação procedural, tela Carteira, proteção da
   demo, deploy. Histórico de posições é o primeiro corte.
2. Rotas de baixa qualidade tornam o mapa pouco crível. Validar a biblioteca
   visualmente antes de integrar.
3. Crescimento de dados na VPS. Retenção e limites de carteira obrigatórios.
4. Visitantes esvaziando ou travando a carteira. Rate limit, intervalo por
   contrato e autocura obrigatórios desde o primeiro deploy.

## Ordem de execução
1. PostgreSQL, migrações, sqlc e troca do store mantendo a interface.
2. Script de biblioteca de rotas e simulador procedural para as duas frotas.
3. Calendário simulado, contratos, perfis de pagador e ciclo de vida.
4. Máquina de bloqueio, outbox e reentrega de desbloqueio.
5. Política, auditoria e rotas HTTP, inclusive SSE `?fleet=`.
6. Proteção da demo pública: rate limit, intervalo por contrato, autocura.
7. Router, abas e tela Carteira.
8. Compose com Caddy, deploy na VPS, README e openapi.yaml.
