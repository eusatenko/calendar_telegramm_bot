package com.usatenko.familycalendar;

import android.appwidget.AppWidgetManager;
import android.content.Context;
import android.content.Intent;
import android.widget.RemoteViews;
import android.widget.RemoteViewsService;

import java.util.Collections;
import java.util.List;

public final class ScheduleRemoteViewsService extends RemoteViewsService {
    @Override
    public RemoteViewsFactory onGetViewFactory(Intent intent) {
        int widgetId = intent.getIntExtra(AppWidgetManager.EXTRA_APPWIDGET_ID,
                AppWidgetManager.INVALID_APPWIDGET_ID);
        return new Factory(getApplicationContext(), widgetId);
    }

    private static final class Factory implements RemoteViewsFactory {
        private final Context context;
        private final int widgetId;
        private List<ScheduleRows.Row> rows = Collections.emptyList();

        Factory(Context context, int widgetId) {
            this.context = context;
            this.widgetId = widgetId;
        }

        @Override public void onCreate() { reload(); }
        @Override public void onDataSetChanged() { reload(); }
        @Override public void onDestroy() { rows = Collections.emptyList(); }
        @Override public int getCount() { return rows.size(); }
        @Override public int getViewTypeCount() { return 2; }
        @Override public boolean hasStableIds() { return true; }
        @Override public long getItemId(int position) { return rows.get(position).text.hashCode(); }
        @Override public RemoteViews getLoadingView() { return null; }

        @Override
        public RemoteViews getViewAt(int position) {
            if (position < 0 || position >= rows.size()) {
                return null;
            }
            return ScheduleRows.remoteView(context, rows.get(position));
        }

        private void reload() {
            rows = ScheduleRows.read(context, widgetId);
        }
    }
}
