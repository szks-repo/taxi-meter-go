package main

import (
	"fmt"
	"log/slog"
	"time"
)

func main() {
	// 料金設定
	config := FareConfig{
		InitialFare:     500,                          // 初乗り500円
		InitialDistance: 1.096,                        // 初乗り1.096km
		UnitFare:        100,                          // 単位料金100円
		UnitDistance:    0.237,                        // 237mごと
		TimeThreshold:   10.0,                         // 10km/h以下で時間制
		TimeUnitFare:    100,                          // 時間制100円
		TimeUnit:        time.Minute + 30*time.Second, // 1分30秒ごと
	}

	// サンプル乗車シミュレーション
	events := []TripEvent{
		{
			EventType: TripEventTypeStart,
			Timestamp: time.Now(),
		},
		{
			EventType: TripEventTypeMove,
			Timestamp: time.Now().Add(2 * time.Minute),
			Distance:  0.8,
			Duration:  2 * time.Minute,
			Speed:     24.0, // 24km/h - 通常走行
		},
		{
			EventType: TripEventTypeStop,
			Timestamp: time.Now().Add(4 * time.Minute),
			Duration:  2 * time.Minute, // 信号待ち2分
		},
		{
			EventType: TripEventTypeMove,
			Timestamp: time.Now().Add(9 * time.Minute),
			Distance:  0.5,
			Duration:  5 * time.Minute,
			Speed:     6.0, // 6km/h - 渋滞
		},
		{
			EventType: TripEventTypeMove,
			Timestamp: time.Now().Add(12 * time.Minute),
			Distance:  0.8,
			Duration:  2 * time.Minute,
			Speed:     24.0, // 再び通常走行
		},
		{
			EventType: TripEventTypeEnd,
			Timestamp: time.Now().Add(12 * time.Minute),
		},
	}

	rideSession := NewRideSession(
		"ride-001",
		Driver{ID: "driver-123", Name: "田中太郎"},
		Passenger{ID: "passenger-456", Name: "佐藤花子"},
		config,
	)

	processResult := processEvents(rideSession, events)
	paymentResult := rideSession.ProcessPayment(PaymentMethodCard, time.Now())
	if !paymentResult.Success {
		slog.Warn("決済エラー", "error", paymentResult.Error)
	}

	slog.Info("=== セッションサマリー ===")
	for key, value := range processResult.SessionInfo {
		slog.Info(fmt.Sprintf("%s: %v", key, value))
	}
}

func (tm *TaxiMeter) ProcessEvent(event TripEvent) EventResult {
	var result EventResult
	result.LogMessages = make([]string, 0)
	oldFare := tm.CurrentFare

	switch event.EventType {
	case TripEventTypeStart:
		meterResult := tm.startTrip(event)
		result = meterResult
	case TripEventTypeMove:
		meterResult := tm.processMovement(event)
		result = meterResult
	case TripEventTypeStop:
		meterResult := tm.processStop(event)
		result = meterResult
	case TripEventTypeEnd:
		meterResult := tm.endTrip(event)
		result = meterResult
	default:
		result.Success = false
		result.Error = fmt.Errorf("unknown event type: %s", event.EventType)
		result.Message = "不明なイベントタイプ"
		return result
	}

	// 料金変更額を計算
	result.FareChange = tm.CurrentFare - oldFare
	result.NewTotalFare = tm.CurrentFare

	return result
}

type EventResult struct {
	Success      bool
	Message      string
	FareChange   int      // 料金の変更額
	NewTotalFare int      // 新しい合計料金
	LogMessages  []string // ログメッセージ
	Error        error
}

type ProcessResult struct {
	EventResults []EventResult
	FinalFare    int
	SessionInfo  map[string]any
}

type TripEvent struct {
	EventType TripEventType // "start", "move", "stop", "end"
	Timestamp time.Time     // イベント発生時刻
	Distance  float64       // この区間での移動距離 (km)
	Duration  time.Duration // この区間での経過時間
	Speed     float64       // この区間での平均速度 (km/h)
}

type TripEventType int

const (
	TripEventTypeStart = iota + 1
	TripEventTypeMove
	TripEventTypeStop
	TripEventTypeEnd
)

type FareConfig struct {
	InitialFare     int           // 初乗り料金
	InitialDistance float64       // 初乗り距離 (km)
	UnitFare        int           // 単位料金
	UnitDistance    float64       // 単位距離 (km)
	TimeThreshold   float64       // 時間制に切り替わる速度閾値 (km/h)
	TimeUnitFare    int           // 時間制単位料金
	TimeUnit        time.Duration // 時間制単位時間
}

type RideSession struct {
	SessionID   string
	Driver      Driver
	Passenger   Passenger
	StartTime   time.Time
	EndTime     *time.Time
	Status      SessionStatus
	Meter       *TaxiMeter
	Events      []TripEvent
	PaymentInfo *PaymentInfo
}

func NewRideSession(sessionID string, driver Driver, passenger Passenger, config FareConfig) *RideSession {
	return &RideSession{
		SessionID: sessionID,
		Driver:    driver,
		Passenger: passenger,
		Status:    StatusWaiting,
		Meter:     NewTaxiMeter(config),
		Events:    make([]TripEvent, 0, 64),
	}
}

// SessionStatus は乗車セッションの状態を表す
type SessionStatus string

const (
	StatusWaiting   SessionStatus = "waiting"    // 配車待ち
	StatusPickingUp SessionStatus = "picking_up" // 迎車中
	StatusOnboard   SessionStatus = "onboard"    // 乗車中
	StatusCompleted SessionStatus = "completed"  // 完了
	StatusCancelled SessionStatus = "cancelled"  // キャンセル
)

type Driver struct {
	ID   string
	Name string
}

type Passenger struct {
	ID   string
	Name string
}

type PaymentInfo struct {
	Method      PaymentMethod
	Amount      int
	ProcessedAt *time.Time
}

type PaymentMethod string

const (
	PaymentMethodCash    = "cash"
	PaymentMethodCard    = "card"
	PaymentMethodDigital = "digital"
)

type TaxiMeter struct {
	Config        FareConfig
	TotalDistance float64
	TotalTime     time.Duration
	CurrentFare   int
	IsRunning     bool
	StartTime     time.Time
	LastEventTime time.Time
}

func NewTaxiMeter(config FareConfig) *TaxiMeter {
	return &TaxiMeter{
		Config: config,
	}
}

func (rs *RideSession) ProcessEvent(event TripEvent) EventResult {
	var result EventResult
	result.LogMessages = make([]string, 0)

	// イベントを記録
	rs.Events = append(rs.Events, event)

	// セッション状態を更新
	switch event.EventType {
	case TripEventTypeStart:
		if rs.Status != StatusWaiting {
			result.Success = false
			result.Error = fmt.Errorf("cannot start ride in status: %s", rs.Status)
			result.Message = "セッション開始に失敗"
			return result
		}
		rs.Status = StatusOnboard
		rs.StartTime = event.Timestamp
		result.LogMessages = append(result.LogMessages, fmt.Sprintf("🚕 セッション開始 (ID: %s)", rs.SessionID))

	case TripEventTypeEnd:
		if rs.Status != StatusOnboard {
			result.Success = false
			result.Error = fmt.Errorf("cannot end ride in status: %s", rs.Status)
			result.Message = "セッション終了に失敗"
			return result
		}
		rs.Status = StatusCompleted
		endTime := event.Timestamp
		rs.EndTime = &endTime
		result.LogMessages = append(result.LogMessages, fmt.Sprintf("🏁 セッション終了 (ID: %s)", rs.SessionID))
	}

	// メータを更新
	meterResult := rs.Meter.ProcessEvent(event)

	if meterResult.Error != nil {
		result.Success = false
		result.Error = meterResult.Error
		result.Message = meterResult.Message
		return result
	}

	// 結果をマージ
	result.Success = true
	result.Message = meterResult.Message
	result.FareChange = meterResult.FareChange
	result.NewTotalFare = rs.Meter.GetCurrentFare()
	result.LogMessages = append(result.LogMessages, meterResult.LogMessages...)

	return result
}

func (rs *RideSession) ProcessPayment(method PaymentMethod, now time.Time) EventResult {
	var result EventResult
	result.LogMessages = make([]string, 0)

	if rs.Status != StatusCompleted {
		result.Success = false
		result.Error = fmt.Errorf("cannot process payment for incomplete ride")
		result.Message = "決済処理に失敗：乗車が完了していません"
		return result
	}

	if rs.PaymentInfo != nil {
		result.Success = false
		result.Error = fmt.Errorf("payment already processed")
		result.Message = "決済処理に失敗：既に決済済みです"
		return result
	}

	rs.PaymentInfo = &PaymentInfo{
		Method:      method,
		Amount:      rs.Meter.GetCurrentFare(),
		ProcessedAt: &now,
	}

	result.Success = true
	result.Message = "決済完了"
	result.NewTotalFare = rs.PaymentInfo.Amount
	result.LogMessages = append(result.LogMessages, fmt.Sprintf("💳 決済完了: %s - %d円", method, rs.PaymentInfo.Amount))

	return result
}

func (rs *RideSession) GetSessionSummary() map[string]any {
	summary := map[string]any{
		"session_id":     rs.SessionID,
		"driver":         rs.Driver.Name,
		"passenger":      rs.Passenger.Name,
		"status":         rs.Status,
		"start_time":     rs.StartTime,
		"end_time":       rs.EndTime,
		"total_distance": rs.Meter.GetTotalDistance(),
		"final_fare":     rs.Meter.GetCurrentFare(),
		"event_count":    len(rs.Events),
	}

	if rs.PaymentInfo != nil {
		summary["payment_method"] = rs.PaymentInfo.Method
		summary["payment_processed"] = rs.PaymentInfo.ProcessedAt
	}

	return summary
}

func (tm *TaxiMeter) startTrip(event TripEvent) EventResult {
	var result EventResult
	result.LogMessages = make([]string, 0)

	if tm.IsRunning {
		result.Success = false
		result.Error = fmt.Errorf("trip already started")
		result.Message = "メータ開始に失敗：既に開始済み"
		return result
	}

	tm.IsRunning = true
	tm.StartTime = event.Timestamp
	tm.LastEventTime = event.Timestamp
	tm.CurrentFare = tm.Config.InitialFare
	tm.TotalDistance = 0
	tm.TotalTime = 0

	result.Success = true
	result.Message = "メータ開始"
	result.LogMessages = append(result.LogMessages, fmt.Sprintf("🚕 乗車開始 - 初乗り料金: %d円", tm.CurrentFare))

	return result
}

func (tm *TaxiMeter) processMovement(event TripEvent) EventResult {
	var result EventResult
	result.LogMessages = make([]string, 0)

	if !tm.IsRunning {
		result.Success = false
		result.Error = fmt.Errorf("trip not started")
		result.Message = "移動処理に失敗：メータが開始されていません"
		return result
	}

	oldFare := tm.CurrentFare
	tm.TotalDistance += event.Distance
	tm.TotalTime += event.Duration
	tm.LastEventTime = event.Timestamp

	// 速度に基づいて料金計算方法を決定
	if event.Speed <= tm.Config.TimeThreshold {
		// 低速時は時間制
		fareInfo := tm.calculateTimeFare(event.Duration)
		tm.CurrentFare += fareInfo.Amount
		result.LogMessages = append(result.LogMessages, fmt.Sprintf("⏱️  低速移動 (%.1f km/h) - 時間制料金加算", event.Speed))
		if fareInfo.Amount > 0 {
			result.LogMessages = append(result.LogMessages, fmt.Sprintf("   時間料金 +%d円 (現在: %d円)", fareInfo.Amount, tm.CurrentFare))
		}
	} else {
		// 通常時は距離制
		fareInfo := tm.calculateDistanceFare(event.Distance)
		tm.CurrentFare += fareInfo.Amount
		result.LogMessages = append(result.LogMessages, fmt.Sprintf("🏃 通常移動 (%.1f km/h) - 距離制料金加算", event.Speed))
		if fareInfo.Amount > 0 {
			result.LogMessages = append(result.LogMessages, fmt.Sprintf("   距離料金 +%d円 (現在: %d円)", fareInfo.Amount, tm.CurrentFare))
		}
	}

	result.Success = true
	result.Message = "移動処理完了"
	result.FareChange = tm.CurrentFare - oldFare

	return result
}

func (tm *TaxiMeter) processStop(event TripEvent) EventResult {
	var result EventResult
	result.LogMessages = make([]string, 0)

	if !tm.IsRunning {
		result.Success = false
		result.Error = fmt.Errorf("trip not started")
		result.Message = "停止処理に失敗：メータが開始されていません"
		return result
	}

	oldFare := tm.CurrentFare
	tm.TotalTime += event.Duration
	tm.LastEventTime = event.Timestamp

	// 停止時間も時間制で加算
	fareInfo := tm.calculateTimeFare(event.Duration)
	tm.CurrentFare += fareInfo.Amount

	result.Success = true
	result.Message = "停止処理完了"
	result.FareChange = tm.CurrentFare - oldFare
	result.LogMessages = append(result.LogMessages, "🛑 停止中 - 時間制料金加算")
	if fareInfo.Amount > 0 {
		result.LogMessages = append(result.LogMessages, fmt.Sprintf("   時間料金 +%d円 (現在: %d円)", fareInfo.Amount, tm.CurrentFare))
	}

	return result
}

func (tm *TaxiMeter) endTrip(event TripEvent) EventResult {
	var result EventResult
	result.LogMessages = make([]string, 0)

	if !tm.IsRunning {
		result.Success = false
		result.Error = fmt.Errorf("trip not started")
		result.Message = "終了処理に失敗：メータが開始されていません"
		return result
	}

	tm.IsRunning = false

	result.Success = true
	result.Message = "メータ終了"
	result.LogMessages = append(result.LogMessages, "🏁 乗車終了")
	result.LogMessages = append(result.LogMessages, tm.generateSummaryMessages()...)

	return result
}

type FareCalculationInfo struct {
	Amount int
	Units  int
	Reason string
}

func (tm *TaxiMeter) calculateDistanceFare(distance float64) FareCalculationInfo {
	if tm.TotalDistance <= tm.Config.InitialDistance {
		return FareCalculationInfo{Amount: 0, Units: 0, Reason: "初乗り距離内"}
	}

	chargeableDistance := tm.TotalDistance - tm.Config.InitialDistance
	units := int(chargeableDistance / tm.Config.UnitDistance)

	// 前回の計算からの差分のみ加算
	previousDistance := tm.TotalDistance - distance
	previousChargeableDistance := previousDistance - tm.Config.InitialDistance
	if previousChargeableDistance < 0 {
		previousChargeableDistance = 0
	}
	previousUnits := int(previousChargeableDistance / tm.Config.UnitDistance)

	additionalUnits := units - previousUnits
	if additionalUnits <= 0 {
		return FareCalculationInfo{Amount: 0, Units: 0, Reason: "追加単位なし"}
	}

	return FareCalculationInfo{
		Amount: additionalUnits * tm.Config.UnitFare,
		Units:  additionalUnits,
		Reason: fmt.Sprintf("%d単位追加", additionalUnits),
	}
}

func (tm *TaxiMeter) calculateTimeFare(duration time.Duration) FareCalculationInfo {
	units := int(duration / tm.Config.TimeUnit)
	if units <= 0 {
		return FareCalculationInfo{Amount: 0, Units: 0, Reason: "時間単位未満"}
	}

	return FareCalculationInfo{
		Amount: units * tm.Config.TimeUnitFare,
		Units:  units,
		Reason: fmt.Sprintf("%d時間単位", units),
	}
}

func (tm *TaxiMeter) generateSummaryMessages() []string {
	return []string{
		fmt.Sprintf("総距離: %.2f km", tm.TotalDistance),
		fmt.Sprintf("総時間: %v", tm.TotalTime),
		fmt.Sprintf("最終料金: %d円", tm.CurrentFare),
	}
}

func (tm *TaxiMeter) GetCurrentFare() int {
	return tm.CurrentFare
}

func (tm *TaxiMeter) GetTotalDistance() float64 {
	return tm.TotalDistance
}

func processEvents(session *RideSession, events []TripEvent) ProcessResult {
	var result ProcessResult
	result.EventResults = make([]EventResult, 0, len(events))

	for _, event := range events {
		eventResult := session.ProcessEvent(event)
		result.EventResults = append(result.EventResults, eventResult)

		for _, msg := range eventResult.LogMessages {
			slog.Info(msg)
		}
		if !eventResult.Success {
			slog.Error("eventResult", "error", eventResult.Error)
		}
	}

	result.FinalFare = session.Meter.GetCurrentFare()
	result.SessionInfo = session.GetSessionSummary()

	return result
}
