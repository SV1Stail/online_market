BEGIN;

DROP TABLE IF EXISTS delivery_place_dates;
DROP TABLE IF EXISTS delivery_places;
DROP TABLE IF EXISTS delivery_day_limits;
DROP TABLE IF EXISTS notification_outbox;
DROP TABLE IF EXISTS order_event_changes;
DROP TABLE IF EXISTS order_events;
-- Вместе с позицией заказа удаляется снимок подписи ценовой единицы.
DROP TABLE IF EXISTS order_items;
DROP TABLE IF EXISTS orders;
DROP TABLE IF EXISTS admin_sessions;
DROP TABLE IF EXISTS admin_user_roles;
DROP TABLE IF EXISTS admin_users;
DROP TABLE IF EXISTS cart_items;
DROP TABLE IF EXISTS carts;
DROP TABLE IF EXISTS product_categories;
-- DROP TABLE IF EXISTS product_images; -- Изображения товаров не входят в MVP.
-- Вместе с товаром удаляются подпись единицы и минимумы заказа.
DROP TABLE IF EXISTS products;
DROP TABLE IF EXISTS categories;

COMMIT;

