# Project Brief: Fleet Pulse

## Executive Summary
Fleet Pulse é um simulador de frota de carros compartilhados conectados.
Dispositivos simulados publicam telemetria por MQTT, um backend em Go ingere
esses dados, mantém o estado da frota e transmite atualizações ao navegador
por Server Sent Events. O operador vê os veículos se movendo em um mapa e
envia comandos de destravamento e travamento que retornam de forma assíncrona.

O projeto é um artefato de portfólio construído em janela curta de tempo para
sustentar uma conversa técnica sobre IoT, sistemas distribuídos e comandos
assíncronos. O objetivo não é um produto comercializável.

## Problem Statement
Em uma frota conectada, o comando enviado a um veículo não tem resposta
imediata nem garantia de entrega. O dispositivo pode estar sem sinal, pode
demorar para responder ou pode falhar a execução. Um sistema que trata esse
comando como uma chamada síncrona mente para o usuário e para a operação.
O problema central deste projeto é representar honestamente o ciclo de vida
de um comando remoto e o estado corrente da frota.

## Proposed Solution
1. Broker MQTT embutido no próprio binário Go.
2. Simulador que instancia entre 15 e 25 veículos, cada um em sua goroutine,
   publicando telemetria a cada 2 segundos.
3. Ingestor que assina os tópicos de telemetria e atualiza um store em
   memória exposto por trás de uma interface.
4. Hub de Server Sent Events com buffer por cliente e descarte sob pressão.
5. Endpoints de destravar e travar que respondem 202 com identificador, publicam
   no tópico de comandos do veículo e acompanham o ack em uma máquina de estados.
   Um segundo comando distinto enquanto outro está em voo entra numa fila de
   profundidade um; o relógio de 5s começa na publicação (SENT).
6. Frontend React com mapa Leaflet, painel de veículo e feed de eventos.

## Target Users
Usuário primário: operador de frota fictício, dentro da narrativa do projeto.
Usuário real: entrevistador técnico avaliando decisões de engenharia.

## Goals and Success Metrics
1. Demo completa executável a partir de um único binário.
2. Ciclo de comando visível ponta a ponta em menos de 5 segundos.
3. Caminho infeliz demonstrável: veículo offline resulta em expiração tratada.
4. Código legível o suficiente para ser explicado linha a linha.

## MVP Scope

### Core Features
1. Broker MQTT embutido e simulador de veículos com telemetria periódica.
2. Store de frota em memória atrás de interface, com leitura concorrente.
3. Endpoint de snapshot da frota.
4. Streaming de atualizações por Server Sent Events.
5. Comandos de destravar e travar com resposta 202, identificador, header
   Idempotency-Key (escopo vin + action) e fila de um comando por veículo.
6. Máquina de estados de comando com os estados PENDING, SENT, ACKED, FAILED
   e TIMEOUT, com expiração de 5 segundos a partir do SENT.
7. Cenários de falha injetados: um veículo permanentemente offline e chance
   de 10% de falha na execução do comando.
8. Frontend com mapa, marcadores vivos, painel do veículo e feed de eventos.
   Copy em português. Tema escuro padrão com toggle claro. Tiles Esri Canvas.
9. Geofence da zona Centro: estado Fora da área, KPI, polígono tracejado e
   evento de saída no feed.
10. Logs estruturados com identificador de correlação propagado do HTTP até o
   ack recebido por MQTT.
11. README com diagrama de arquitetura e seção de trade offs.

### Out of Scope for MVP
Autenticação e autorização, persistência em banco, múltiplas réplicas,
histórico de trajetos, reserva ou aluguel de veículo, cobrança, testes
end to end, responsividade mobile, deploy em nuvem.

### MVP Success Criteria
O avaliador abre uma URL, vê a frota se movendo (incluindo um veículo saindo
da área Centro), destrava um veículo e acompanha a mudança de estado, trava
de volta, depois tenta destravar o veículo offline e vê a expiração tratada
com mensagem clara.

## Post MVP Vision
Métricas Prometheus com latência de ack, persistência de last known state em
Redis, histórico em PostgreSQL com PostGIS, fan out de eventos via NATS para
permitir múltiplas réplicas do servidor de streaming, client TypeScript
gerado a partir do OpenAPI.

## Technical Considerations

### Backend
Go, módulo único com pacotes internos separados por responsabilidade.
Broker MQTT embutido via mochi-mqtt server v2. Dispositivos simulados
conectam como clientes MQTT reais. HTTP com a biblioteca padrão.
Logs com log/slog em JSON. Encerramento gracioso com signal.NotifyContext,
drenando clientes de streaming antes de finalizar.

### Frontend
React 19 com TypeScript, Vite, react-leaflet v5 e CSS puro ou CSS modules.
Sem router, sem biblioteca de estado, sem biblioteca de componentes.
Estado da frota em useReducer indexado por identificador de veículo.
Conexão única de EventSource protegida contra montagem dupla do StrictMode.
Build estático incorporado no binário com go:embed e fallback para index.html.

### Tópicos MQTT
1. fleet/{vin}/telemetry, publicado pelo dispositivo.
2. fleet/{vin}/commands, publicado pelo servidor.
3. fleet/{vin}/ack, publicado pelo dispositivo.

### API HTTP
1. GET /api/vehicles retorna o snapshot da frota, o polígono permitido e o
   id curto de apresentação. VIN é o identificador da API e do MQTT.
2. GET /api/stream entrega o fluxo de eventos por SSE. O hub descarta o
   evento mais antigo sob pressão. Sem compressão nesta rota.
3. POST /api/vehicles/{vin}/unlock aceita Idempotency-Key e responde 202
   com o identificador do comando.
4. POST /api/vehicles/{vin}/lock usa a mesma máquina, idempotência e fila.
5. GET /api/commands/{id} retorna o estado corrente do comando.
6. GET /healthz para verificação de saúde.
Contrato documentado em openapi.yaml.

## Constraints and Assumptions
1. Janela de desenvolvimento de aproximadamente 4 horas, uma pessoa.
2. Entrega precisa rodar sem dependência externa obrigatória.
3. Dados são inteiramente simulados, não há hardware envolvido.
4. Estado em memória é aceito de forma consciente e documentada.

## Risks and Open Questions
1. Risco de estourar o tempo no polimento do mapa. Mitigação: mapa funcional
   antes de qualquer ajuste visual.
2. Risco de conflito de peer dependencies entre React 19 e react-leaflet.
   Mitigação: instalar as versões alinhadas juntas e, em caso de atrito,
   usar Leaflet imperativo dentro de um useRef.
3. Risco de buffer em proxy quebrar o SSE em desenvolvimento. Mitigação:
   desabilitar compressão na rota de streaming.
4. Travamento, geofence, copy em português, tiles Esri e tema claro/escuro
   estão no MVP. Decisões em `_bmad-output/specs/spec-fleet-pulse/`.

## Ordem de execução sugerida
1. Backend com broker, simulador e telemetria em log.
2. Store, snapshot e streaming validados por linha de comando.
3. Mapa renderizando com marcadores vivos.
4. Destravar ponta a ponta com máquina de estados e expiração; depois travar
   e a fila de um.
5. Painel, feed, falhas, geofence e tema.
6. README, contrato OpenAPI e empacotamento.
