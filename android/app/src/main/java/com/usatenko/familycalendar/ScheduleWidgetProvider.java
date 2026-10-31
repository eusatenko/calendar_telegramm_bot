package com.usatenko.familycalendar;

import android.appwidget.AppWidgetManager;
import android.appwidget.AppWidgetProvider;
import android.content.Context;
import android.content.Intent;
import android.os.Bundle;

import java.util.concurrent.atomic.AtomicInteger;

public final class ScheduleWidgetProvider extends AppWidgetProvider {
    static final String ACTION_REFRESH = "com.usatenko.familycalendar.REFRESH";
    static final String ACTION_TODAY = "com.usatenko.familycalendar.TODAY";
    static final String ACTION_TOMORROW = "com.usatenko.familycalendar.TOMORROW";
    static final String ACTION_TOGGLE_MODE = "com.usatenko.familycalendar.TOGGLE_MODE";

    @Override
    public void onUpdate(Context context, AppWidgetManager manager, int[] widgetIds) {
        PendingResult pending = goAsync();
        if (widgetIds.length == 0) {
            pending.finish();
            return;
        }
        AtomicInteger remaining = new AtomicInteger(widgetIds.length);
        for (int widgetId : widgetIds) {
            WidgetUpdater.refresh(context, widgetId, () -> {
                if (remaining.decrementAndGet() == 0) {
                    pending.finish();
                }
            });
        }
    }

    @Override
    public void onAppWidgetOptionsChanged(Context context, AppWidgetManager manager,
                                          int widgetId, Bundle newOptions) {
        WidgetUpdater.render(context, widgetId, false);
    }

    @Override
    public void onReceive(Context context, Intent intent) {
        super.onReceive(context, intent);
        String action = intent.getAction();
        if (!ACTION_REFRESH.equals(action) && !ACTION_TODAY.equals(action)
                && !ACTION_TOMORROW.equals(action) && !ACTION_TOGGLE_MODE.equals(action)) {
            return;
        }
        int widgetId = intent.getIntExtra(AppWidgetManager.EXTRA_APPWIDGET_ID,
                AppWidgetManager.INVALID_APPWIDGET_ID);
        if (widgetId == AppWidgetManager.INVALID_APPWIDGET_ID) {
            return;
        }
        if (ACTION_TODAY.equals(action)) {
            WidgetPrefs.setDay(context, widgetId, "today");
        } else if (ACTION_TOMORROW.equals(action)) {
            WidgetPrefs.setDay(context, widgetId, "tomorrow");
        } else if (ACTION_TOGGLE_MODE.equals(action)) {
            String next = "chronological".equals(WidgetPrefs.mode(context, widgetId))
                    ? "people" : "chronological";
            WidgetPrefs.setMode(context, widgetId, next);
        }
        PendingResult pending = goAsync();
        WidgetUpdater.refresh(context, widgetId, pending::finish);
    }

    @Override
    public void onDeleted(Context context, int[] widgetIds) {
        for (int widgetId : widgetIds) {
            WidgetPrefs.deleteWidget(context, widgetId);
        }
    }
}
