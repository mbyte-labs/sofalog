package ro.mbyte.sofalog.ui.stats

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Rect
import androidx.compose.ui.geometry.RoundRect
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.drawText
import androidx.compose.ui.text.rememberTextMeasurer
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import ro.mbyte.sofalog.domain.StatsHeatmap
import ro.mbyte.sofalog.domain.plural
import java.time.LocalDate
import java.time.YearMonth
import java.time.format.DateTimeFormatter
import java.util.Locale
import kotlin.math.ceil

private val monthShort = DateTimeFormatter.ofPattern("MMM", Locale.ENGLISH)
private val monthYear = DateTimeFormatter.ofPattern("MMM yyyy", Locale.ENGLISH)
private val weekdayDay = DateTimeFormatter.ofPattern("EEE d MMM yyyy", Locale.ENGLISH)

/** A YYYY-MM bucket as "Oct", or "Oct 2026" in full. No zone is involved, so none can move it. */
internal fun formatStatsMonth(month: String, short: Boolean): String =
    runCatching { YearMonth.parse(month).format(if (short) monthShort else monthYear) }.getOrDefault(month)

/**
 * The page's one chart: a single series of vertical bars. There is no hover on a touch screen, so
 * a tap on a bar's column selects it and its [detail] is written under the chart — the web chart's
 * tooltip, where a thumb can read it. [description] names the chart for a screen reader, which is
 * then read every bar's label and value.
 */
@Composable
internal fun StatsBarChart(
    values: List<Float>,
    labels: List<String>,
    description: String,
    detail: (Int) -> String,
    modifier: Modifier = Modifier,
    height: Dp = 180.dp,
    fixedMax: Float? = null,
    colors: List<Color>? = null,
    thinLabels: Boolean = false,
) {
    var selected by remember(values, labels) { mutableStateOf<Int?>(null) }
    val measurer = rememberTextMeasurer()
    val barColor = MaterialTheme.colorScheme.primary
    val gridColor = MaterialTheme.colorScheme.outlineVariant
    val highlight = MaterialTheme.colorScheme.onSurface.copy(alpha = 0.06f)
    val tickStyle = MaterialTheme.typography.labelSmall.copy(color = MaterialTheme.colorScheme.onSurfaceVariant)

    // Whole-number ticks, at most five: the axis of a count never reads "2.5 plays".
    val step = niceStep((fixedMax ?: (values.maxOrNull() ?: 0f)) / 5f)
    val top = fixedMax ?: (ceil((values.maxOrNull() ?: 0f) / step) * step).coerceAtLeast(step)
    val ticks = generateSequence(0f) { it + step }.takeWhile { it <= top + 0.001f }.toList()
    val spoken = labels.zip(values).joinToString(", ") { (label, value) -> "$label: ${trimNumber(value)}" }

    Column(modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Canvas(
            Modifier
                .fillMaxWidth()
                .height(height)
                .semantics { contentDescription = "$description. $spoken" }
                .pointerInput(values.size) {
                    detectTapGestures { tap ->
                        val left = measurer.measure(trimNumber(top), tickStyle).size.width + 8.dp.toPx()
                        val slot = (size.width - left) / values.size.coerceAtLeast(1)
                        val index = ((tap.x - left) / slot).toInt()
                        selected = if (tap.x >= left && index in values.indices && index != selected) index else null
                    }
                },
        ) {
            if (values.isEmpty()) return@Canvas
            val left = measurer.measure(trimNumber(top), tickStyle).size.width + 8.dp.toPx()
            val bottom = 20.dp.toPx()
            val topPad = 8.dp.toPx()
            val plotHeight = size.height - bottom - topPad
            val slot = (size.width - left) / values.size
            fun y(value: Float) = topPad + plotHeight * (1f - value / top)

            for (tick in ticks) {
                val line = y(tick)
                drawLine(gridColor, Offset(left, line), Offset(size.width, line), strokeWidth = 1.dp.toPx())
                val text = measurer.measure(trimNumber(tick), tickStyle)
                drawText(text, topLeft = Offset(left - 8.dp.toPx() - text.size.width, line - text.size.height / 2f))
            }
            selected?.let { i ->
                drawRect(highlight, Offset(left + slot * i, topPad), Size(slot, plotHeight))
            }
            val barWidth = minOf(slot * 0.72f, 32.dp.toPx())
            val radius = CornerRadius(4.dp.toPx())
            values.forEachIndexed { i, value ->
                val x = left + slot * i + (slot - barWidth) / 2f
                if (value > 0f) {
                    val bar = Path().apply {
                        addRoundRect(
                            RoundRect(
                                Rect(x, y(value.coerceAtMost(top)), x + barWidth, topPad + plotHeight),
                                topLeft = radius,
                                topRight = radius,
                            ),
                        )
                    }
                    drawPath(bar, colors?.getOrNull(i) ?: barColor)
                }
                if (!thinLabels || i % 2 == 0) {
                    val text = measurer.measure(labels.getOrElse(i) { "" }, tickStyle)
                    drawText(text, topLeft = Offset(left + slot * i + (slot - text.size.width) / 2f, size.height - text.size.height))
                }
            }
        }
        Caption(selected?.takeIf { it in values.indices }?.let(detail) ?: "Tap a bar for its number")
    }
}

/** The smallest of 1, 2, 5, 10, 20, 50, … that is at least [raw]: an axis counts in steps a reader expects. */
private fun niceStep(raw: Float): Float {
    var magnitude = 1f
    while (true) {
        for (factor in listOf(1f, 2f, 5f)) if (factor * magnitude >= raw) return factor * magnitude
        magnitude *= 10f
    }
}

/** 3 → "3", 7.5 → "7.5": an axis or a spoken value without a pointless ".0". */
private fun trimNumber(value: Float): String =
    if (value == value.toInt().toFloat()) value.toInt().toString() else "%.1f".format(Locale.ENGLISH, value)

private val CellSize = 14.dp
private val CellGap = 3.dp
private val MonthRow = 18.dp

/** The five shades of a day, none to busiest: the surface, then the primary colour at rising strength. */
@Composable
private fun heatColors(): List<Color> {
    val primary = MaterialTheme.colorScheme.primary
    return listOf(
        MaterialTheme.colorScheme.surfaceContainerHighest,
        primary.copy(alpha = 0.25f),
        primary.copy(alpha = 0.45f),
        primary.copy(alpha = 0.70f),
        primary,
    )
}

/**
 * One cell per day, one column per week with Monday on top, scrolled to the newest week. The
 * weekday gutter stays put while the weeks scroll. A tap picks a day and names it underneath.
 */
@Composable
internal fun PlaysHeatmap(heatmap: StatsHeatmap, modifier: Modifier = Modifier) {
    val start = remember(heatmap.start) { runCatching { LocalDate.parse(heatmap.start) }.getOrNull() } ?: return
    val days = heatmap.plays.size
    // Monday-first: how many empty cells lead the first column.
    val lead = start.dayOfWeek.value - 1
    val weeks = (lead + days + 6) / 7
    var selected by remember(heatmap) { mutableStateOf<Int?>(null) }
    val scroll = rememberScrollState()
    // Open on the newest week that has something in it — the end of a rolling year, but not the
    // empty months still ahead in the current one.
    val density = LocalDensity.current
    val newest = heatmap.plays.indexOfLast { it > 0 }
    LaunchedEffect(heatmap.start, days, newest, scroll.maxValue, scroll.viewportSize) {
        val target = if (newest < 0) {
            scroll.maxValue
        } else {
            val pitch = with(density) { (CellSize + CellGap).toPx() }
            // Two weeks of room after the newest column, so it does not sit on the edge.
            (((lead + newest) / 7 + 3) * pitch - scroll.viewportSize).toInt()
        }
        scroll.scrollTo(target.coerceIn(0, scroll.maxValue))
    }

    val colors = heatColors()
    val outline = MaterialTheme.colorScheme.onSurface
    val measurer = rememberTextMeasurer()
    val labelStyle = MaterialTheme.typography.labelSmall.copy(color = MaterialTheme.colorScheme.onSurfaceVariant)
    val active = heatmap.plays.count { it > 0 }
    val total = heatmap.plays.sum()

    Column(modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(10.dp)) {
        Row {
            Column(Modifier.padding(top = MonthRow).width(34.dp)) {
                for (row in 0 until 7) {
                    Box(Modifier.height(CellSize + CellGap), contentAlignment = Alignment.CenterStart) {
                        if (row == 0 || row == 2 || row == 4) {
                            Text(listOf("Mon", "", "Wed", "", "Fri")[row], style = labelStyle)
                        }
                    }
                }
            }
            Box(Modifier.weight(1f).horizontalScroll(scroll)) {
                Canvas(
                    Modifier
                        .size(width = (CellSize + CellGap) * weeks, height = MonthRow + (CellSize + CellGap) * 7)
                        .semantics {
                            contentDescription = "${plural(total, "play", "plays")} on ${plural(active, "day", "days")}"
                        }
                        .pointerInput(heatmap) {
                            detectTapGestures { tap ->
                                val pitch = (CellSize + CellGap).toPx()
                                val column = (tap.x / pitch).toInt()
                                val row = ((tap.y - MonthRow.toPx()) / pitch).toInt()
                                val index = column * 7 + row - lead
                                selected = if (tap.y >= MonthRow.toPx() && row in 0..6 && index in 0 until days && index != selected) index else null
                            }
                        },
                ) {
                    val pitch = (CellSize + CellGap).toPx()
                    val cell = CellSize.toPx()
                    val corner = CornerRadius(3.dp.toPx())
                    var lastLabelColumn = -3
                    for (i in 0 until days) {
                        val column = (lead + i) / 7
                        val row = (lead + i) % 7
                        val day = start.plusDays(i.toLong())
                        // A month is named over the column holding its 1st, unless that crowds the last name.
                        if (day.dayOfMonth == 1 && column - lastLabelColumn >= 3) {
                            drawText(measurer.measure(day.format(monthShort), labelStyle), topLeft = Offset(column * pitch, 0f))
                            lastLabelColumn = column
                        }
                        val origin = Offset(column * pitch, MonthRow.toPx() + row * pitch)
                        drawRoundRect(colors[heatmap.levels.getOrElse(i) { 0 }.coerceIn(0, 4)], origin, Size(cell, cell), corner)
                        if (i == selected) {
                            drawRoundRect(outline, origin, Size(cell, cell), corner, style = Stroke(2.dp.toPx()))
                        }
                    }
                }
            }
        }
        Caption(
            selected?.takeIf { it in 0 until days }?.let { i ->
                "${start.plusDays(i.toLong()).format(weekdayDay)} · ${plural(heatmap.plays[i], "play", "plays")}"
            } ?: "Tap a day for its plays",
        )
        Row(verticalAlignment = Alignment.CenterVertically) {
            Caption("${plural(total, "play", "plays")} on ${plural(active, "day", "days")}", Modifier.weight(1f))
            Caption("Less")
            Spacer(Modifier.width(6.dp))
            Row(horizontalArrangement = Arrangement.spacedBy(3.dp)) {
                colors.forEach { Box(Modifier.size(10.dp).clip(RoundedCornerShape(2.dp)).background(it)) }
            }
            Spacer(Modifier.width(6.dp))
            Caption("More")
        }
    }
}
