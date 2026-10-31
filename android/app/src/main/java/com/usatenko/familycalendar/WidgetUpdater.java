package com.usatenko.familycalendar;

import android.app.PendingIntent;
import android.appwidget.AppWidgetManager;
import android.content.ComponentName;
import android.content.Context;
import android.content.Intent;
import android.net.Uri;
import android.os.Build;
import android.view.View;
import android.widget.RemoteViews;

import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;

final class WidgetUpdater {
    private static final ExecutorService EXECUTOR = Executors.newFixedThreadPool(2);

    private WidgetUpdater() {}

    static void refresh(Context context, int widgetId, Runnable finished) {
        Context appContext = context.getApplicationContext();
        render(appContext, widgetId, true);
        EXECUTOR.execute(() -> {
            try {
                ApiClient.Result result = ApiClient.fetch(appContext, widgetId);
                WidgetPrefs.saveResult(appContext, widgetId, result.json, result.title);
            } catch (Exception error) {
                String message = error.getMessage();
                WidgetPrefs.saveError(appContext, widgetId,
                        message == null || message.trim().isEmpty() ? "Не удалось обновить" : message);
            }
            render(appContext, widgetId, false);
            if (Build.VERSION.SDK_INT < Build.VERSION_CODES.S) {
                AppWidgetManager.getInstance(appContext).notifyAppWidgetViewDataChanged(widgetId, R.id.widget_list);
            }
            if (finished != null) {
                finished.run();
            }
        });
    }

    static void refreshAll(Context context) {
        AppWidgetManager manager = AppWidgetManager.getInstance(context);
        int[] ids = manager.getAppWidgetIds(new ComponentName(context, ScheduleWidgetProvider.class));
        for (int id : ids) {
            refresh(context, id, null);
        }
    }

    static void render(Context context, int widgetId, boolean loading) {
        AppWidgetManager manager = AppWidgetManager.getInstance(context);
        RemoteViews views = new RemoteViews(context.getPackageName(), R.layout.widget_schedule);
        boolean configured = WidgetPrefs.token(context).length() >= 32;
        views.setTextViewText(R.id.widget_title, WidgetPrefs.title(context, widgetId));
        views.setTextViewText(R.id.button_today,
                "today".equals(WidgetPrefs.day(context, widgetId)) ? "• Сегодня" : "Сегодня");
        views.setTextViewText(R.id.button_tomorrow,
                "tomorrow".equals(WidgetPrefs.day(context, widgetId)) ? "• Завтра" : "Завтра");
        views.setTextViewText(R.id.button_mode,
                "chronological".equals(WidgetPrefs.mode(context, widgetId)) ? "По людям" : "По времени");
        views.setOnClickPendingIntent(R.id.button_today, action(context, widgetId, ScheduleWidgetProvider.ACTION_TODAY, 1));
        views.setOnClickPendingIntent(R.id.button_tomorrow, action(context, widgetId, ScheduleWidgetProvider.ACTION_TOMORROW, 2));
        views.setOnClickPendingIntent(R.id.button_mode, action(context, widgetId, ScheduleWidgetProvider.ACTION_TOGGLE_MODE, 3));
        views.setOnClickPendingIntent(R.id.button_refresh, action(context, widgetId, ScheduleWidgetProvider.ACTION_REFRESH, 4));

        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            setDirectCollection(context, widgetId, views);
        } else {
            Intent serviceIntent = new Intent(context, ScheduleRemoteViewsService.class);
            serviceIntent.putExtra(AppWidgetManager.EXTRA_APPWIDGET_ID, widgetId);
            serviceIntent.setData(Uri.parse(serviceIntent.toUri(Intent.URI_INTENT_SCHEME)));
            views.setRemoteAdapter(R.id.widget_list, serviceIntent);
        }
        views.setEmptyView(R.id.widget_list, R.id.widget_empty);

        String error = WidgetPrefs.error(context, widgetId);
        if (!configured) {
            error = "Откройте Family Calendar и настройте доступ";
        }
        views.setTextViewText(R.id.widget_error, loading ? "Обновление…" : error);
        views.setViewVisibility(R.id.widget_error, loading || !error.isEmpty() ? View.VISIBLE : View.GONE);
        manager.updateAppWidget(widgetId, views);
    }

    @android.annotation.SuppressLint("NewApi") // Caller guards this with SDK_INT >= S.
    private static void setDirectCollection(Context context, int widgetId, RemoteViews views) {
        RemoteViews.RemoteCollectionItems.Builder items = new RemoteViews.RemoteCollectionItems.Builder()
                .setViewTypeCount(2);
        java.util.List<ScheduleRows.Row> rows = ScheduleRows.read(context, widgetId);
        for (int position = 0; position < rows.size(); position++) {
            ScheduleRows.Row row = rows.get(position);
            long id = ((long) row.text.hashCode() << 32) ^ position;
            items.addItem(id, ScheduleRows.remoteView(context, row));
        }
        views.setRemoteAdapter(R.id.widget_list, items.build());
    }

    private static PendingIntent action(Context context, int widgetId, String action, int offset) {
        Intent intent = new Intent(context, ScheduleWidgetProvider.class);
        intent.setAction(action);
        intent.putExtra(AppWidgetManager.EXTRA_APPWIDGET_ID, widgetId);
        return PendingIntent.getBroadcast(context, widgetId * 10 + offset, intent,
                PendingIntent.FLAG_UPDATE_CURRENT | PendingIntent.FLAG_IMMUTABLE);
    }
}
