use crate::kline::{Direction, Kline, MergedKline};

/// 处理K线包含关系，返回合并后的K线序列
pub fn process_contain(klines: &[Kline]) -> Vec<MergedKline> {
    if klines.is_empty() {
        return vec![];
    }
    let mut result: Vec<MergedKline> = Vec::with_capacity(klines.len());
    result.push(MergedKline {
        index: 0,
        open_time: klines[0].open_time,
        close_time: klines[0].close_time,
        high: klines[0].high,
        low: klines[0].low,
        direction: Direction::Neutral,
        merged_count: 1,
    });

    for i in 1..klines.len() {
        let k = &klines[i];
        let dir = current_direction(&result, k);
        let last = result.last_mut().unwrap();

        if is_contain(last.high, last.low, k.high, k.low) {
            merge(last, k, dir);
        } else {
            result.push(MergedKline {
                index: i,
                open_time: k.open_time,
                close_time: k.close_time,
                high: k.high,
                low: k.low,
                direction: dir,
                merged_count: 1,
            });
        }
    }
    result
}

fn is_contain(h1: f64, l1: f64, h2: f64, l2: f64) -> bool {
    (h1 >= h2 && l1 <= l2) || (h2 >= h1 && l2 <= l1)
}

fn merge(last: &mut MergedKline, k: &Kline, dir: Direction) {
    match dir {
        Direction::Up => {
            // 向上合并：取高的高点，取高的低点
            last.high = last.high.max(k.high);
            last.low = last.low.max(k.low);
        }
        _ => {
            // 向下合并：取低的高点，取低的低点
            last.high = last.high.min(k.high);
            last.low = last.low.min(k.low);
        }
    }
    last.close_time = k.close_time;
    last.merged_count += 1;
}

fn current_direction(result: &[MergedKline], k: &Kline) -> Direction {
    if result.len() < 2 {
        if k.high > result.last().unwrap().high {
            return Direction::Up;
        }
        return Direction::Down;
    }
    let prev = &result[result.len() - 2];
    let last = &result[result.len() - 1];
    if last.high > prev.high {
        Direction::Up
    } else {
        Direction::Down
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn k(high: f64, low: f64) -> Kline {
        Kline { open_time: 0, close_time: 0, open: low, high, low, close: high, volume: 0.0 }
    }

    #[test]
    fn test_no_contain() {
        let klines = vec![k(10.0, 5.0), k(12.0, 7.0), k(15.0, 9.0)];
        let merged = process_contain(&klines);
        assert_eq!(merged.len(), 3);
    }

    #[test]
    fn test_contain_up() {
        // K2 包含 K1（向上趋势中）
        let klines = vec![k(10.0, 5.0), k(12.0, 7.0), k(13.0, 6.0)];
        let merged = process_contain(&klines);
        // K2 和 K3 有包含关系，向上合并
        assert_eq!(merged.len(), 2);
        assert_eq!(merged[1].high, 13.0);
        assert_eq!(merged[1].low, 7.0); // 向上取高的低点
    }

    #[test]
    fn test_contain_down() {
        // 向下趋势中的包含
        let klines = vec![k(15.0, 10.0), k(12.0, 7.0), k(11.0, 8.0)];
        let merged = process_contain(&klines);
        assert_eq!(merged.len(), 2);
        assert_eq!(merged[1].high, 11.0); // 向下取低的高点
        assert_eq!(merged[1].low, 7.0);
    }
}
