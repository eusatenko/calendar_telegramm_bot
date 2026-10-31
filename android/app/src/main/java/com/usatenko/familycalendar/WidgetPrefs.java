package com.usatenko.familycalendar;

import android.content.Context;
import android.content.SharedPreferences;

final class WidgetPrefs {
    static final String DEFAULT_SERVER_URL = "https://usatenko-bf.com";
    private static final String FILE = "family_calendar_widget";
    private static final String SERVER_URL = "server_url";
    private static final String API_TOKEN = "api_token";

    private WidgetPrefs() {}

    static SharedPreferences prefs(Context context) {
        return context.getSharedPreferences(FILE, Context.MODE_PRIVATE);
    }

    static String serverUrl(Context context) {
        return prefs(context).getString(SERVER_URL, DEFAULT_SERVER_URL);
    }

    static String token(Context context) {
        return prefs(context).getString(API_TOKEN, "");
    }

    static void saveConnection(Context context, String serverUrl, String token) {
        prefs(context).edit().putString(SERVER_URL, serverUrl).putString(API_TOKEN, token).apply();
    }

    static String day(Context context, int widgetId) {
        return prefs(context).getString("day_" + widgetId, "today");
    }

    static String mode(Context context, int widgetId) {
        return prefs(context).getString("mode_" + widgetId, "chronological");
    }

    static void setDay(Context context, int widgetId, String day) {
        prefs(context).edit().putString("day_" + widgetId, day).apply();
    }

    static void setMode(Context context, int widgetId, String mode) {
        prefs(context).edit().putString("mode_" + widgetId, mode).apply();
    }

    static String cache(Context context, int widgetId) {
        return prefs(context).getString("cache_" + widgetId, "");
    }

    static String title(Context context, int widgetId) {
        return prefs(context).getString("title_" + widgetId, "Семейное расписание");
    }

    static String error(Context context, int widgetId) {
        return prefs(context).getString("error_" + widgetId, "");
    }

    static void saveResult(Context context, int widgetId, String json, String title) {
        prefs(context).edit().putString("cache_" + widgetId, json)
                .putString("title_" + widgetId, title).remove("error_" + widgetId).apply();
    }

    static void saveError(Context context, int widgetId, String error) {
        prefs(context).edit().putString("error_" + widgetId, error).apply();
    }

    static void deleteWidget(Context context, int widgetId) {
        prefs(context).edit().remove("day_" + widgetId).remove("mode_" + widgetId)
                .remove("cache_" + widgetId).remove("title_" + widgetId)
                .remove("error_" + widgetId).apply();
    }
}
