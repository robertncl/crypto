import { useEffect, useRef, useState } from "react";
import {
  createChart, CandlestickSeries, HistogramSeries, ColorType, CrosshairMode, LineStyle,
  type IChartApi, type ISeriesApi, type UTCTimestamp,
} from "lightweight-charts";
import { api } from "../api/client";
import type { Candle, Market } from "../api/types";
import { useChannel } from "../hooks/useStream";
import { fmt, stepDecimals, toNum } from "../utils/format";
import { Tabs, tabPanelProps } from "./acme";
import { Panel } from "./common";

const INTERVALS = [
  { id: "1m", label: "1m", sec: 60 },
  { id: "5m", label: "5m", sec: 300 },
  { id: "15m", label: "15m", sec: 900 },
  { id: "1h", label: "1h", sec: 3600 },
  { id: "4h", label: "4h", sec: 14400 },
  { id: "1d", label: "1D", sec: 86400 },
] as const;
type IntervalId = (typeof INTERVALS)[number]["id"];

interface Ohlc { o: number; h: number; l: number; c: number }

// Reads the ACME tokens off the root element so the chart has no colour system
// of its own. Candles use the market direction pair (Blue up, hollow; Crimson
// down, filled) and the last-price line is the system's data highlight.
function chartColors() {
  const s = getComputedStyle(document.documentElement);
  const v = (name: string) => s.getPropertyValue(name).trim();
  return {
    up: v("--acme-color-info"),
    down: v("--acme-color-danger"),
    hollow: v("--acme-color-surface-raised"),
    text: v("--acme-color-text-muted"),
    grid: v("--acme-color-surface"),
    axis: v("--acme-color-border"),
    volume: v("--acme-color-border"),
    last: v("--acme-color-data-highlight"),
    font: v("--font-sans"),
  };
}

function candlePoint(c: Candle) {
  return {
    time: c.time as UTCTimestamp,
    open: toNum(c.open), high: toNum(c.high), low: toNum(c.low), close: toNum(c.close),
  };
}

function volumePoint(c: Candle) {
  return { time: c.time as UTCTimestamp, value: toNum(c.volume) };
}

function toOhlc(c: Candle): Ohlc {
  return { o: toNum(c.open), h: toNum(c.high), l: toNum(c.low), c: toNum(c.close) };
}

export function Chart({ market }: { market: Market }) {
  const wrapRef = useRef<HTMLDivElement>(null);
  const chartRef = useRef<IChartApi | null>(null);
  const candleRef = useRef<ISeriesApi<"Candlestick"> | null>(null);
  const volRef = useRef<ISeriesApi<"Histogram"> | null>(null);
  const lastRef = useRef<Ohlc | null>(null);
  const hoverRef = useRef(false);
  const [interval, setInterval] = useState<IntervalId>("15m");
  const [ohlc, setOhlc] = useState<Ohlc | null>(null);

  const pd = stepDecimals(market.priceTick);
  const sec = INTERVALS.find((i) => i.id === interval)?.sec ?? 900;

  // Create the chart once.
  useEffect(() => {
    const el = wrapRef.current;
    if (!el) return;
    const c = chartColors();
    const chart = createChart(el, {
      autoSize: true,
      layout: {
        background: { type: ColorType.Solid, color: "transparent" },
        textColor: c.text,
        fontFamily: c.font,
      },
      grid: { vertLines: { color: c.grid }, horzLines: { color: c.grid } },
      crosshair: { mode: CrosshairMode.Normal },
      rightPriceScale: { borderColor: c.axis },
      timeScale: { borderColor: c.axis, timeVisible: true, secondsVisible: false },
    });
    const candles = chart.addSeries(CandlestickSeries, {
      upColor: c.hollow, downColor: c.down,
      borderVisible: true, borderUpColor: c.up, borderDownColor: c.down,
      wickUpColor: c.up, wickDownColor: c.down,
      priceLineColor: c.last, priceLineStyle: LineStyle.Dashed,
    });
    const vol = chart.addSeries(HistogramSeries, {
      color: c.volume,
      priceFormat: { type: "volume" },
      priceScaleId: "",
      lastValueVisible: false,
      priceLineVisible: false,
    });
    vol.priceScale().applyOptions({ scaleMargins: { top: 0.84, bottom: 0 } });

    // The header readout follows the crosshair, falling back to the live candle.
    chart.subscribeCrosshairMove((param) => {
      const d = param.seriesData.get(candles);
      if (d && "open" in d) {
        hoverRef.current = true;
        setOhlc({ o: d.open, h: d.high, l: d.low, c: d.close });
      } else {
        hoverRef.current = false;
        setOhlc(lastRef.current);
      }
    });

    chartRef.current = chart;
    candleRef.current = candles;
    volRef.current = vol;
    return () => {
      chart.remove();
      chartRef.current = null;
      candleRef.current = null;
      volRef.current = null;
    };
  }, []);

  // Price precision follows the market's tick; axis labels get thousands separators.
  useEffect(() => {
    chartRef.current?.applyOptions({ localization: { priceFormatter: (p: number) => fmt(p, pd) } });
    candleRef.current?.applyOptions({ priceFormat: { type: "price", precision: pd, minMove: toNum(market.priceTick) } });
  }, [pd, market.priceTick]);

  // Load historical candles when market or interval changes.
  useEffect(() => {
    let live = true;
    api.candles(market.symbol, interval, 400).then((rows: Candle[]) => {
      if (!live || !candleRef.current || !volRef.current) return;
      candleRef.current.setData(rows.map(candlePoint));
      volRef.current.setData(rows.map(volumePoint));
      chartRef.current?.timeScale().fitContent();
      const last = rows.at(-1);
      lastRef.current = last ? toOhlc(last) : null;
      if (!hoverRef.current) setOhlc(lastRef.current);
    }).catch(() => {});
    return () => { live = false; };
  }, [market.symbol, interval]);

  // Live candle updates for the selected interval.
  useChannel<Candle>(`kline:${market.symbol}:${sec}`, (c) => {
    candleRef.current?.update(candlePoint(c));
    volRef.current?.update(volumePoint(c));
    lastRef.current = toOhlc(c);
    if (!hoverRef.current) setOhlc(lastRef.current);
  });

  return (
    <Panel
      id="chart"
      title="Price chart"
      tabbed
      aside={
        <Tabs
          tabs={INTERVALS.map((i) => ({ id: i.id, label: i.label }))}
          active={interval}
          onChange={setInterval}
          idPrefix="interval"
          label="Chart interval"
        />
      }
    >
      <div {...tabPanelProps("interval", interval)}>
        <p className="chart__ohlc">
          {ohlc ? `O ${fmt(ohlc.o, pd)}  H ${fmt(ohlc.h, pd)}  L ${fmt(ohlc.l, pd)}  C ${fmt(ohlc.c, pd)}` : " "}
        </p>
        <div
          className="chart"
          ref={wrapRef}
          role="img"
          aria-label={`${market.symbol} ${interval} candlestick chart`}
        />
      </div>
    </Panel>
  );
}
