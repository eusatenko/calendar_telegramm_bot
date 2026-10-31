package com.usatenko.familycalendar;

import android.appwidget.AppWidgetManager;
import android.content.Context;
import android.content.Intent;
import android.view.View;
import android.widget.RemoteViews;
import android.widget.RemoteViewsService;

import org.json.JSONArray;
import org.json.JSONObject;

import java.util.ArrayList;
import java.util.List;

public final class ScheduleRemoteViewsService extends RemoteViewsService {
    @Override
    public RemoteViewsFactory onGetViewFactory(Intent intent) {
        int widgetId = intent.getIntExtra(AppWidgetManager.EXTRA_APPWIDGET_ID,
                AppWidgetManager.INVALID_APPWIDGET_ID);
        return new Factory(getApplicationContext(), widgetId);
    }

    private static final class Row {
        final String kind;
        final String text;
        final String location;

        Row(String kind, String text, String location) {
            this.kind = kind;
            this.text = text;
            this.location = location;
        }
    }

    private static final class Factory implements RemoteViewsFactory {
        private final Context context;
        private final int widgetId;
        private final List<Row> rows = new ArrayList<>();

        Factory(Context context, int widgetId) {
            this.context = context;
            this.widgetId = widgetId;
        }

        @Override public void onCreate() { reload(); }
        @Override public void onDataSetChanged() { reload(); }
        @Override public void onDestroy() { rows.clear(); }
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
            Row row = rows.get(position);
            if ("header".equals(row.kind)) {
                RemoteViews views = new RemoteViews(context.getPackageName(), R.layout.widget_row_header);
                views.setTextViewText(R.id.row_header, row.text);
                return views;
            }
            RemoteViews views = new RemoteViews(context.getPackageName(), R.layout.widget_row_event);
            views.setTextViewText(R.id.row_text, row.text);
            views.setTextViewText(R.id.row_location, row.location);
            views.setViewVisibility(R.id.row_location, row.location.isEmpty() ? View.GONE : View.VISIBLE);
            return views;
        }

        private void reload() {
            rows.clear();
            String cached = WidgetPrefs.cache(context, widgetId);
            if (cached.isEmpty()) {
                return;
            }
            try {
                JSONArray values = new JSONObject(cached).getJSONArray("rows");
                for (int i = 0; i < values.length(); i++) {
                    JSONObject value = values.getJSONObject(i);
                    rows.add(new Row(value.getString("kind"), value.getString("text"),
                            value.optString("location", "")));
                }
            } catch (Exception ignored) {
                rows.clear();
            }
        }
    }
}
