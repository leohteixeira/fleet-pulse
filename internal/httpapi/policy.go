package httpapi

import (
	"strings"
	"time"
	"unicode/utf8"
)

const (
	codeLateBelow16    = "late_below_16"
	codeNotifyRequired = "notify_required"
	codeNotifyRecent   = "notify_too_recent"
	codeDeviceOffline  = "device_offline"
	codeReasonInvalid  = "reason_invalid"
	codeInDay          = "in_day"
	codeNotifyCooldown = "notify_cooldown"
	codePaymentNotDue  = "payment_not_due"
	codeInTransit      = "in_transit"

	minReasonRunes    = 8
	maxReasonRunes    = 280
	notifyCooldown    = 24 * time.Hour
	blockNotifyAge    = 48 * time.Hour
	minBlockDaysLate  = 16
	minNotifyDaysLate = 1
)

type policyError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *policyError) Error() string {
	if e == nil {
		return ""
	}
	return e.Code
}

func notifyPolicy(daysLate int, lastNotify, simNow time.Time) *policyError {
	if daysLate < minNotifyDaysLate {
		return &policyError{Code: codeInDay, Message: "contrato em dia; notificacao nao permitida"}
	}
	if !lastNotify.IsZero() && simNow.Sub(lastNotify) < notifyCooldown {
		return &policyError{Code: codeNotifyCooldown, Message: "aguarde 24 horas simuladas entre notificacoes"}
	}
	return nil
}

func blockPolicy(daysLate int, lastNotify, simNow time.Time, isOnline bool, reason string) *policyError {
	if err := reasonPolicy(reason); err != nil {
		return err
	}
	if daysLate < minBlockDaysLate {
		return &policyError{Code: codeLateBelow16, Message: "bloqueio exige pelo menos 16 dias de atraso"}
	}
	if lastNotify.IsZero() {
		return &policyError{Code: codeNotifyRequired, Message: "notificacao previa e obrigatoria"}
	}
	if simNow.Sub(lastNotify) < blockNotifyAge {
		return &policyError{Code: codeNotifyRecent, Message: "notificacao deve ter pelo menos 48 horas simuladas"}
	}
	if !isOnline {
		return &policyError{Code: codeDeviceOffline, Message: "dispositivo offline"}
	}
	return nil
}

func reasonPolicy(reason string) *policyError {
	n := utf8.RuneCountInString(strings.TrimSpace(reason))
	if n < minReasonRunes || n > maxReasonRunes {
		return &policyError{Code: codeReasonInvalid, Message: "motivo deve ter entre 8 e 280 caracteres"}
	}
	return nil
}

func inTransitError() *policyError {
	return &policyError{Code: codeInTransit, Message: "comando em transito"}
}

func paymentNotDueError() *policyError {
	return &policyError{Code: codePaymentNotDue, Message: "nenhuma parcela vencida ou a vencer em 5 dias"}
}
