# Project Brief: Fleet Pulse, fase 2, Leasing e Persistência

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
e oferta da mesma tecnologia a bancos como SaaS.

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
1. Aluguel (fase 1): cerca de 20 veículos, viagens aleatórias.
2. Leasing (fase 2): cerca de 50 veículos, cada um vinculado a um contrato
   e a um cliente com rotina própria.
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

## Calendário simulado
1. SIM_DAY_SECONDS configurável, padrão 300.
2. Data simulada exposta na API e no header do frontend.
3. Perfis de pagador atribuídos aleatoriamente com pesos: pontual, atrasa
   ocasionalmente, regulariza após notificação, inadimplente.
4. Pagamentos gerados pelo perfil a cada vencimento.
5. Ciclo de vida: contratos terminam, novos são criados, carteira mantida
   entre 40 e 60 contratos ativos.

## Leasing
1. Contrato: cliente, veículo, valor da parcela, total de parcelas, parcelas
   pagas, perfil de pagador, datas.
2. Faixas de atraso: em dia, 1 a 15, 16 a 30, acima de 30 dias simulados.
3. Ações: notificar cliente, solicitar bloqueio com motivo obrigatório,
   cancelar bloqueio, registrar pagamento manual.
4. Política validada no servidor: mínimo de 30 dias em atraso e notificação
   registrada há pelo menos 3 dias simulados. Violação retorna 422 com
   código de erro legível.
5. Máquina de estados do bloqueio: REQUESTED, ARMED, SENT, ACKED, CANCELLED,
   FAILED, TIMEOUT. ARMED só avança para SENT quando a telemetria indica
   velocidade zero e ignição desligada.
6. Pagamento cancela bloqueio em REQUESTED ou ARMED e emite desbloqueio para
   veículo bloqueado.
7. Desbloqueio com dispositivo offline fica pendente e é reentregue na
   reconexão.
8. Veículo bloqueado não liga a ignição nem se move no simulador.
9. Auditoria append only: timestamp real, data simulada, origem (visitante
   ou sistema), ação, motivo, estado anterior, estado novo, correlation id.
   Para visitantes, registrar apenas um identificador anônimo derivado do IP
   com hash, nunca o IP em claro.

## Proteção da demo pública
1. Sem login. Todas as ações disponíveis para qualquer visitante.
2. Rate limit por IP nas rotas de escrita, com resposta 429 e header
   Retry-After.
3. Intervalo mínimo entre ações no mesmo contrato, independente do
   visitante, com resposta 409 e código de erro legível.
4. Idempotency-Key obrigatória nas rotas de escrita.
5. Autocura: o sistema regulariza contratos segundo o perfil de pagador
   mesmo após intervenção de visitantes, e nenhum veículo permanece
   bloqueado por mais que um limite configurável de dias simulados.
6. Proporção mínima garantida de contratos em cada faixa de atraso, para
   que sempre exista algo interessante na tela.
7. Limite de conexões SSE simultâneas.
8. Broker MQTT acessível apenas na rede interna.
9. Headers de segurança e CORS restrito ao próprio domínio.
10. Payloads validados com limite de tamanho, motivo com limite de
    caracteres.

## Persistência
1. PostgreSQL, driver pgx, queries com sqlc, migrações com goose.
2. Tabelas: vehicles, vehicle_state, routes, customers, contracts,
   installments, payments, commands, audit_log, outbox.
3. vehicle_state com upsert e cache em memória na frente.
4. Histórico de posições apenas para leasing, amostrado por minuto,
   retenção de 7 dias por job de limpeza. Item opcional, cortar se faltar
   tempo.
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
9. Eventos de contrato e bloqueio no stream SSE, separados por canal de
   frota.
Atualizar openapi.yaml.

## Frontend
Abas Frota e Carteira no header, sem router. Carteira com mapa dos carros
financiados, tabela de contratos, indicadores, filtros, painel de detalhe com
parcelas e auditoria, diálogo de confirmação de bloqueio. Data simulada no
header. Tratamento de 409, 422 e 429 com mensagens claras. Design entregue
separadamente como tokens e especificação.

## Deploy
1. Docker compose com app, PostgreSQL e Caddy com HTTPS automático.
2. Binário único com frontend embutido.
3. Healthcheck da app e do banco.
4. Migrações executadas na inicialização.
5. Logs estruturados em JSON.

## Critérios de aceite
1. Após uma hora rodando, trajetos e eventos não se repetem de forma
   perceptível.
2. Contratos entram e saem da inadimplência sozinhos.
3. Bloqueio sem notificação prévia é recusado com mensagem clara.
4. Bloqueio solicitado com carro em movimento fica ARMED até o carro parar.
5. Carro bloqueado para no mapa da Carteira.
6. Pagamento libera o carro, inclusive após reconexão de dispositivo offline.
7. Um visitante disparando ações em sequência recebe 429 e a demo se
   recupera sozinha em poucos minutos.
8. Reiniciar o container preserva contratos, comandos e auditoria.
9. Uso de disco estável ao longo de dias.

## Fora de escopo
Autenticação, juros e multa, boletos, notificação real ao cliente, aprovação
em duas etapas, multi tenant, testes end to end, alta disponibilidade.

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
5. Política, auditoria e rotas HTTP.
6. Proteção da demo pública: rate limit, intervalo por contrato, autocura.
7. Tela Carteira.
8. Compose com Caddy, deploy na VPS, README e openapi.yaml.
