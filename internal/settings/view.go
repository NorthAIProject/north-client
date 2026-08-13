package settings

import (
	"github.com/NorthAIProject/north-client/internal/preferences"
	"github.com/NorthAIProject/north-client/internal/users"
	settingspages "github.com/NorthAIProject/north-client/web/settings"
)

func buildSettingsSummary(user users.User, unitsSystem string, dietCount, connectionCount int) settingspages.SettingsSummary {
	units := "Metric"
	if unitsSystem == preferences.UnitsImperial {
		units = "Imperial"
	}

	tz := user.Timezone
	if tz == "" {
		tz = "—"
	}

	return settingspages.SettingsSummary{
		Timezone:        tz,
		Units:           units,
		DietCount:       dietCount,
		ConnectionCount: connectionCount,
	}
}
