PROMPTS
--------

feat: vamos mudar o layout de como os checks são apresentados.
Coloque-os um sobre o outro na esquerda e os graficos ao lado. Use apenas 5 linhas para cada.
A esquerda todos os dados relevantes até a coluna 30 e a partir dai o chart.

---

feat:
Organize os charts nas seguintes sessões:
Machine: 
  CPU
  Memory
  Disk Usage
  Disk I/O
Network:
  Throughput
  PING
  DNS Lookup
  TTFB
  REQUEST

feat:
Crie um /show que liste os titulos dos charts que são apresentados e opção de selecionar os que são mostrados/ocultos.

feat:
Vamos criar uma especie de service discovery no tui. A ideia central é termos um conf com os serviços que queremos monitorar. Mas precisamos de comandos que busquem na maquina quais serviços estão rodando e criar os charts para monitorar o uso de cpu deles. Aproveite e adicione na conf os itens que estão marcados como show true/false no tui tambem.
A apresentação deverá ser MACHINE E NETWORK na tela principal e SERVICES em uma segunda tela que o usuario irá trocar com uma tecla de atalho. As informações precisam se manter ao trocar de "telas".


feat:
Crie uma entrada no makefile para buildar o binario e disponiblizar em um tar.gz

feat:
o tui deve criar um arquivo de conf .toml com os serviços, thresholds etc configurados para quando sair e entrar denovo use estes parametros. Além disso ao executar o ./status --conf <conf-file> deve ser possivel carregar as confs de um setting especifico.

---
feat:
ao fazer o /service liste os serviços por default e permita selecionar serviços que serão removidos do monitoramento pela listagem

---
feat:
crie uma linha na aba MAIN chamado EDGE (abaixo do MACHINE e NETWORK). Ele deverá monitorar unicamente a EDGE que as requisições HTTP usadas para o TTFB estão atingindo. da seguinte forma:
Faça a requisição com o HEADER  -H 'Pragma: azion-debug-cache'.
Com isso receberemos os seguintes Headers:
< x-azion-edge-location: IAD
< x-ea-rule-last-modified: 2026-02-08 03:16:15.570902+00:00
< x-ef-rule-last-modified: 2026-01-19 20:42:50.459507+00:00
< x-azion-edge-pop: EQN
Vamos apresentar o grafico de EDGE em linha de tempo informando o: 
status: 200, 404, etc
loc-pop: Exemplo: IAD-EQN (composição o x-azion-edge-location e x-azion-edge-pop)
orch-latency: tempo que levou para orquestração. Este calculo deve ser assim:
Pegar o valor mais recente de:
x-ea-rule-last-modified: 2026-02-08 03:16:15.570902+00:00
x-ef-rule-last-modified: 2026-01-19 20:42:50.459507+00:00
A cada curl, verificar se o valor mudou. Quando mudar, calcular o tempo em segundos entre o valor mais recente deste header e o tempo da maquina (em UTC-0). Este é o tempo que levou par a atualização propagar.
Importante que na primeira chamada, esta diferença pode ser muito grande então exiba como "-", no momento que houver uma atualização enquanto o tui está monitorando, apresente o valor calculado.
Todas essas infos devem ser exibidas em linha de tempo.