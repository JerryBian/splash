package store

import (
	"context"
	"fmt"
	"math/rand"
	"time"
	"uuid"
)

// seedDays is how much history the demo data covers: enough for a full
// heatmap and meaningful streaks, small enough to insert in one transaction.
const seedDays = 120

// SeedDemo fills an empty database with sample categories, projects and a
// believable history, so a first run has something to look at. It is a no-op
// when any project already exists, and it never touches real data.
//
// The randomness is seeded from a constant: two fresh installs produce the
// same demo, which makes screenshots and tests reproducible.
func (d *DB) SeedDemo(ctx context.Context) (int, error) {
	n, err := d.Projects().Count(ctx)
	if err != nil {
		return 0, err
	}
	if n > 0 {
		return 0, nil
	}

	rng := rand.New(rand.NewSource(7))
	today := d.Today()
	start := AddDays(today, -(seedDays - 1))
	cst := time.FixedZone("CST", 8*3600)

	cats := []CategoryInput{
		{Name: "健康", Icon: "💪", Color: "green"},
		{Name: "学习", Icon: "📖", Color: "blue"},
		{Name: "生活", Icon: "🏡", Color: "orange"},
	}
	catIDs := map[string]string{}
	for _, in := range cats {
		c, err := d.Categories().Create(ctx, in)
		if err != nil {
			return 0, fmt.Errorf("seed category: %w", err)
		}
		catIDs[c.Name] = c.ID.String()
	}

	type demoProject struct {
		in    ProjectInput
		today string // "done", "partial", "pending" or "" (follow the schedule)
	}
	demos := []demoProject{
		{in: ProjectInput{Name: "喝水", Description: "每天 8 杯水，少量多次。", Icon: "💧", Color: "blue", CategoryID: catIDs["健康"], Schedule: ScheduleDaily, Target: 8, Unit: "杯", StartDate: start}, today: "partial"},
		{in: ProjectInput{Name: "冥想", Description: "10 分钟呼吸练习，开始一天。", Icon: "🧘", Color: "teal", CategoryID: catIDs["健康"], Schedule: ScheduleDaily, Target: 1, Unit: "次", StartDate: start}, today: "done"},
		{in: ProjectInput{Name: "晨跑", Description: "每周跑 3 次，每次 3 公里以上。", Icon: "🏃", Color: "green", CategoryID: catIDs["健康"], Schedule: ScheduleWeekly, Target: 3, Unit: "次", StartDate: start}},
		{in: ProjectInput{Name: "力量训练", Description: "周一、三、五，推拉腿分化。", Icon: "🏋️", Color: "red", CategoryID: catIDs["健康"], Private: true, Schedule: ScheduleWeekdays, Target: 1, Unit: "次", Weekdays: []int{1, 3, 5}, StartDate: start}},
		{in: ProjectInput{Name: "站立活动", Description: "每 2 小时起身活动一次，久坐提醒。", Icon: "🚶", Color: "yellow", CategoryID: catIDs["健康"], Schedule: ScheduleIntervalHours, Interval: 2, StartDate: start}},
		{in: ProjectInput{Name: "阅读", Description: "每天 30 分钟，纸质书优先。", Icon: "📚", Color: "purple", CategoryID: catIDs["学习"], Schedule: ScheduleDaily, Target: 30, Unit: "分钟", StartDate: start}, today: "pending"},
		{in: ProjectInput{Name: "背单词", Description: "每天 50 个，复习优先于新词。", Icon: "🧠", Color: "pink", CategoryID: catIDs["学习"], Private: true, Schedule: ScheduleDaily, Target: 50, Unit: "个", StartDate: start}, today: "done"},
		{in: ProjectInput{Name: "每周复盘", Description: "周日晚回顾本周，写三条收获。", Icon: "📝", Color: "orange", CategoryID: catIDs["学习"], Schedule: ScheduleWeekly, Target: 1, Unit: "次", StartDate: start}},
		{in: ProjectInput{Name: "记账", Description: "记录当天每一笔支出。", Icon: "💰", Color: "teal", CategoryID: catIDs["生活"], Schedule: ScheduleDaily, Target: 1, Unit: "次", StartDate: start}, today: "pending"},
		{in: ProjectInput{Name: "护肤", Description: "晚间护肤流程。", Icon: "🌙", Color: "pink", CategoryID: catIDs["生活"], Private: true, Schedule: ScheduleDaily, Target: 1, Unit: "次", StartDate: start}},
	}

	notes := []string{"状态不错", "有点累，但完成了", "顺便听了播客", "和朋友一起", "差点忘了", "补上昨天落下的"}
	moods := Moods

	// Create the projects through the pool BEFORE opening the transaction:
	// with a single connection, a tx holds the only connection the pool could
	// hand out, so a pool query inside an open tx would deadlock forever.
	created := make([]Project, 0, len(demos))
	for _, demo := range demos {
		p, err := d.Projects().Create(ctx, demo.in)
		if err != nil {
			return 0, fmt.Errorf("seed project %s: %w", demo.in.Name, err)
		}
		created = append(created, p)
	}

	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("seed begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	insertCheckin := func(pid uuid.UUID, date string, hour, minute int, value float64, note, mood string) error {
		t, err := ParseDate(date)
		if err != nil {
			return err
		}
		now := time.Date(t.Year(), t.Month(), t.Day(), hour, minute, 0, 0, cst).UTC()
		_, err = tx.ExecContext(ctx, `INSERT INTO checkins (`+checkinColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			uuid.NewV7().String(), pid.String(), value, note, mood, date, toMillis(now), 480, toMillis(now))
		return err
	}
	insertSkip := func(pid uuid.UUID, date, reason string) error {
		t, _ := ParseDate(date)
		now := time.Date(t.Year(), t.Month(), t.Day(), 9, 0, 0, 0, cst).UTC()
		_, err := tx.ExecContext(ctx, `INSERT INTO skips (id, project_id, skip_date, reason, created_at) VALUES (?, ?, ?, ?, ?)`,
			uuid.NewV7().String(), pid.String(), date, reason, toMillis(now))
		return err
	}

	for i, p := range created {
		demo := demos[i]
		for date := start; date <= today; date = AddDays(date, 1) {
			isToday := date == today
			// A rare excuse day, never on the current day.
			if !isToday && rng.Float64() < 0.03 {
				if err := insertSkip(p.ID, date, "休息一天"); err != nil {
					return 0, err
				}
				continue
			}

			switch p.Schedule {
			case ScheduleIntervalHours:
				count := 4 + rng.Intn(3)
				if isToday {
					count = 3
				}
				for i := 0; i < count; i++ {
					hour := 8 + i*2 + rng.Intn(2)
					if hour > 22 {
						hour = 22
					}
					if err := insertCheckin(p.ID, date, hour, rng.Intn(60), 1, "", ""); err != nil {
						return 0, err
					}
				}
			case ScheduleWeekdays:
				if !ScheduledOn(p, date) {
					continue
				}
				if isToday && demo.today == "" {
					continue
				}
				if !isToday && rng.Float64() > 0.8 {
					continue
				}
				if err := insertCheckin(p.ID, date, 18+rng.Intn(3), rng.Intn(60), 1, pick(rng, notes, 0.2), pick(rng, moods, 0.4)); err != nil {
					return 0, err
				}
			case ScheduleWeekly:
				// ~target check-ins per week, spread over random days.
				chance := float64(p.Target) / 7
				if isToday {
					if demo.today == "done" {
						chance = 1
					} else if demo.today == "pending" {
						chance = 0
					}
				}
				if rng.Float64() > chance*0.95 {
					continue
				}
				if err := insertCheckin(p.ID, date, 7+rng.Intn(12), rng.Intn(60), 1, pick(rng, notes, 0.2), pick(rng, moods, 0.4)); err != nil {
					return 0, err
				}
			default: // daily
				if isToday {
					switch demo.today {
					case "pending":
						continue
					case "partial":
						part := p.Target * 0.6
						if p.Unit == "分钟" || p.Unit == "个" || p.Unit == "杯" {
							part = float64(int(part))
						}
						if part < 1 {
							part = 1
						}
						if err := insertCheckin(p.ID, date, 9, 15, part, "", ""); err != nil {
							return 0, err
						}
						continue
					case "done":
						if err := insertCheckin(p.ID, date, 8, 30, p.Target, "", "😄"); err != nil {
							return 0, err
						}
						continue
					}
				}
				if rng.Float64() > 0.82 {
					continue
				}
				value := p.Target * (0.75 + rng.Float64()*0.4)
				if p.Unit == "分钟" || p.Unit == "个" || p.Unit == "杯" {
					value = float64(int(value))
				}
				if value < 1 {
					value = 1
				}
				if err := insertCheckin(p.ID, date, 7+rng.Intn(14), rng.Intn(60), value, pick(rng, notes, 0.15), pick(rng, moods, 0.35)); err != nil {
					return 0, err
				}
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("seed commit: %w", err)
	}
	return len(created), nil
}

// pick returns a random element with probability p, else "".
func pick(rng *rand.Rand, options []string, p float64) string {
	if rng.Float64() > p || len(options) == 0 {
		return ""
	}
	return options[rng.Intn(len(options))]
}
