export const COPY = {
  wordmark: 'SP · cobrança',
  tabFrota: 'Frota',
  tabCarteira: 'Carteira',
  clockPrefix: 'Data simulada',
  kpiActive: 'Contratos ativos',
  kpiOverdue: 'Valor total em atraso',
  kpiArmed: 'Bloqueios armados',
  kpiBlocked: 'Veículos bloqueados',
  kpiReminder: 'Bloqueio só é efetivado com o veículo parado e ignição desligada.',
  kpiLate: (n: number) => `${n} em atraso`,
  kpiInstallments: (n: number) => `${n} parcelas`,
  kpiArmedSub: 'aguardando parada',
  kpiUnlockPending: (n: number) => `${n} desbloq. pendente`,
  filterLate: 'Atraso',
  filterVehicle: 'Veículo',
  searchPlaceholder: 'Placa ou cliente',
  visibleOf: (visible: number, total: number) => `${visible} de ${total} contratos`,
  bandAll: 'Todos',
  bandOk: 'Em dia',
  band115: '1–15 dias',
  band1630: '16–30 dias',
  band30: '> 30 dias',
  vsAll: 'Todos',
  vsAtivo: 'Ativo',
  vsArmado: 'Bloqueio armado',
  vsBloqueado: 'Bloqueado',
  vsUnlock: 'Desbloqueio pendente',
  emptyTitle: 'Nenhum contrato corresponde aos filtros',
  emptySearch: (q: string) => `Nenhuma placa ou cliente contém “${q}”.`,
  emptyHint: 'Combine outra faixa de atraso ou estado do veículo.',
  clearFilters: 'Limpar filtros',
  colClient: 'Cliente',
  colPlate: 'Placa · modelo',
  colInst: 'Parcela',
  colDue: 'Próx. venc.',
  colLate: 'Atraso',
  colVehicle: 'Veículo',
  colSignal: 'Sinal',
  onTime: 'em dia',
  online: 'online',
  offline: 'offline',
  pendingRequested: 'solicitado',
  pendingArmed: 'aguardando parar',
  pendingSent: 'enviado · ACK',
  pendingUnlock: 'desbloqueio enviado',
  panelEmpty: 'Nenhum contrato selecionado',
  panelEmptyHint:
    'Selecione uma linha da tabela ou um marcador no mapa para ver o contrato e agir.',
  panelEmptyCount: (n: number) => `${n} contratos com 16+ dias e veículo ativo`,
  factOverdue: 'Em atraso',
  factInst: 'Parcela',
  factPaid: 'Parcelas pagas',
  factNext: (date: string) => `próx. ${date}`,
  vehicleState: 'Estado do veículo',
  motion: 'Movimento',
  ignition: 'Ignição',
  lastPos: 'Última posição',
  lastSignal: 'Último sinal',
  immobilized: 'IMOBILIZADO',
  moving: (n: number) => `EM MOVIMENTO · ${n} km/h`,
  stopped: 'PARADO',
  ignOn: 'LIGADA',
  ignOff: 'DESLIGADA',
  hist: 'Histórico de parcelas',
  audit: 'Auditoria',
  instPaid: 'paga',
  instDue: 'vencida',
  instOpen: 'aberta',
  notify: 'Notificar cliente',
  block: 'Solicitar bloqueio',
  blockArmed: 'Bloqueio armado',
  blockArmedHint: 'Aguardando veículo parar para efetivar',
  sending: 'Enviando…',
  cancel: 'Cancelar bloqueio',
  pay: 'Registrar pagamento',
  command: 'COMANDO',
  idleNotify: 'nenhuma notificação enviada',
  lastNotify: (date: string) => `última ${date}`,
  idleBlock: 'exige confirmação e motivo',
  idleCancel: 'envia desbloqueio',
  idlePay: 'quita a parcela mais antiga',
  dialogTitle: 'Solicitar bloqueio remoto',
  dialogClient: 'Cliente',
  dialogVehicle: 'Veículo',
  dialogOverdue: 'Em atraso',
  dialogNotify: 'Notificação prévia',
  dialogNever: 'nunca',
  dialogReason: 'Motivo',
  dialogRequired: 'obrigatório',
  dialogPlaceholder:
    'Ex.: 3 tentativas de contato sem retorno; cliente ciente do risco de bloqueio.',
  dialogCounter: (n: number) => `${n} / mín. 8 caracteres`,
  dialogNoticeTitle: 'O bloqueio não é imediato.',
  dialogNotice:
    'O comando fica armado e só é efetivado quando o veículo estiver parado com a ignição desligada. Até então o cliente pode circular normalmente e você pode cancelar.',
  dialogCancel: 'Cancelar',
  dialogConfirm: 'Confirmar solicitação de bloqueio',
  legendAtivo: 'Ativo',
  legendArmed: 'Bloqueio armado',
  legendBlocked: 'Bloqueado',
  legendUnlock: 'Desbloqueio pendente',
  legendOffline: 'Offline',
  legendChip: 'etiqueta: contrato · dias em atraso',
  inDay: 'Contrato em dia',
  lateBelow: (n: number) => `Política: exige 16+ dias em atraso (atual ${n})`,
  notifyRequired: 'Política: notificação prévia obrigatória',
  notifyRecent: 'Política: notificação há menos de 48h',
  notifyCooldown: 'Notificado há menos de 24h',
  deviceOffline: 'Dispositivo offline · comando não chegaria',
  alreadyBlocked: 'Veículo já bloqueado',
  unlockPending: 'Desbloqueio pendente',
  noActiveBlock: 'Nenhum bloqueio ativo ou armado',
  paymentNotDue: 'Sem parcela vencida ou a vencer em 5 dias',
  settled: 'Contrato quitado',
  inTransit: 'Comando em trânsito',
  reasonInvalid: 'Motivo deve ter pelo menos 8 caracteres',
  interval: (n: number) => `Aguardando intervalo entre ações · ${n}s`,
  rateLimit: (n: number) => `Limite de requisições atingido · libera em ${n}s`,
  originVisitor: 'VISITANTE',
  originSystem: 'SISTEMA',
} as const;

export type WriteKind = 'policy' | 'interval' | 'rate';

export type WriteRefusal = {
  kind: WriteKind;
  code: string;
  copy: string;
  retryAfter: number;
};

export function policyCopy(code: string, daysLate = 0): string {
  switch (code) {
    case 'in_day':
      return COPY.inDay;
    case 'late_below_16':
      return COPY.lateBelow(daysLate);
    case 'notify_required':
      return COPY.notifyRequired;
    case 'notify_too_recent':
      return COPY.notifyRecent;
    case 'notify_cooldown':
      return COPY.notifyCooldown;
    case 'device_offline':
      return COPY.deviceOffline;
    case 'reason_invalid':
      return COPY.reasonInvalid;
    case 'payment_not_due':
      return COPY.paymentNotDue;
    case 'in_transit':
      return COPY.inTransit;
    default:
      return code;
  }
}

export function runeCount(value: string): number {
  return [...value].length;
}

export function mapWriteError(
  status: number,
  body: unknown,
  retryAfterHeader?: string | null,
  daysLate = 0,
): WriteRefusal | null {
  const rec = isRecord(body) ? body : {};
  const code = typeof rec.code === 'string' ? rec.code : '';
  const fromBody = typeof rec.retryAfter === 'number' ? rec.retryAfter : NaN;
  const fromHeader = retryAfterHeader ? Number(retryAfterHeader) : NaN;
  const retryAfter = Number.isFinite(fromBody)
    ? fromBody
    : Number.isFinite(fromHeader)
      ? fromHeader
      : 0;

  if (status === 429 || code === 'rate_limited') {
    return { kind: 'rate', code: code || 'rate_limited', copy: COPY.rateLimit(retryAfter), retryAfter };
  }
  if (status === 409 || code === 'contract_busy') {
    return {
      kind: 'interval',
      code: code || 'contract_busy',
      copy: COPY.interval(retryAfter),
      retryAfter,
    };
  }
  if (status === 422) {
    return { kind: 'policy', code, copy: policyCopy(code, daysLate), retryAfter: 0 };
  }
  return null;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value);
}

export const COMMAND_LINE: Record<
  string,
  { label: string; tone: 'idle' | 'accent' | 'ok' | 'err' | 'warn'; dot: 'live' | 'armed' | null }
> = {
  none: { label: 'OCIOSO', tone: 'idle', dot: null },
  REQUESTED: { label: 'SOLICITADO', tone: 'accent', dot: 'live' },
  ARMED: { label: 'ARMADO · aguardando veículo parar', tone: 'warn', dot: 'armed' },
  SENT: { label: 'ENVIADO · aguardando ACK', tone: 'accent', dot: 'live' },
  ACKED: { label: 'CONFIRMADO', tone: 'ok', dot: null },
  CANCELLED: { label: 'CANCELADO', tone: 'idle', dot: null },
  FAILED: { label: 'FALHOU', tone: 'err', dot: null },
  TIMEOUT: { label: 'EXPIRADO', tone: 'warn', dot: null },
};

export function commandLineCopy(
  state: string | null,
  action?: string,
): { label: string; tone: 'idle' | 'accent' | 'ok' | 'err' | 'warn'; dot: 'live' | 'armed' | null } {
  if (!state) {
    return COMMAND_LINE.none ?? { label: 'OCIOSO', tone: 'idle', dot: null };
  }
  const base = COMMAND_LINE[state] ?? COMMAND_LINE.none;
  const row = base ?? { label: 'OCIOSO', tone: 'idle' as const, dot: null };
  if (action === 'unlock' && state !== 'ACKED' && state !== 'CANCELLED' && state !== 'FAILED') {
    return { ...row, label: `DESBLOQUEIO · ${row.label}` };
  }
  return row;
}
